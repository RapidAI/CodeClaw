#!/usr/bin/env python3
"""
Verify the exact SpeechBrain fbank pipeline for ECAPA-TDNN.
Compares SpeechBrain's Fbank output with a pure-numpy reimplementation
to ensure we understand every step before porting to C++.

Usage:
  pip install speechbrain torchaudio soundfile
  python verify_speechbrain_fbank.py --wav <path_to_wav>
"""

import argparse
import numpy as np
import sys
import os


def speechbrain_fbank(wav_path):
    """Compute fbank using SpeechBrain's actual pipeline (ground truth)."""
    import torch
    import torchaudio
    from speechbrain.lobes.features import Fbank
    from speechbrain.processing.features import InputNormalization

    signal, sr = torchaudio.load(wav_path)
    if sr != 16000:
        signal = torchaudio.functional.resample(signal, sr, 16000)

    # SpeechBrain ECAPA-TDNN hyperparams.yaml:
    #   compute_features: Fbank(n_mels=80)
    #   mean_var_norm: InputNormalization(norm_type=sentence, std_norm=False)
    compute_features = Fbank(n_mels=80)
    mean_var_norm = InputNormalization(norm_type="sentence", std_norm=False)

    with torch.no_grad():
        feats = compute_features(signal)  # [1, T, 80]
        # InputNormalization needs lengths
        lens = torch.tensor([1.0])
        feats = mean_var_norm(feats, lens)

    return feats.squeeze(0).numpy()  # [T, 80]


def numpy_speechbrain_fbank(wav_path):
    """Pure numpy reimplementation of SpeechBrain's fbank pipeline.
    This is what we'll port to C++.
    """
    import soundfile as sf
    audio, sr = sf.read(wav_path)
    if len(audio.shape) > 1:
        audio = audio[:, 0]
    if sr != 16000:
        ratio = 16000 / sr
        n_out = int(len(audio) * ratio)
        audio = np.interp(np.linspace(0, len(audio)-1, n_out),
                          np.arange(len(audio)), audio)
    audio = audio.astype(np.float32)

    # SpeechBrain STFT defaults:
    #   sample_rate=16000, win_length=25ms=400, hop_length=10ms=160
    #   n_fft=400, window=hamming, center=True, pad_mode='constant'
    n_fft = 400
    win_length = 400
    hop_length = 160
    n_mels = 80
    f_min = 0.0
    f_max = 8000.0
    sample_rate = 16000

    # Hamming window (torch.hamming_window matches periodic=True)
    # torch.hamming_window(N) = 0.54 - 0.46 * cos(2*pi*n/N) for n=0..N-1
    window = 0.54 - 0.46 * np.cos(2 * np.pi * np.arange(win_length) / win_length)

    # Center padding (same as torch.stft center=True, pad_mode='constant')
    pad_len = n_fft // 2
    audio_padded = np.pad(audio, (pad_len, pad_len), mode='constant')

    # Frame extraction
    n_frames = 1 + (len(audio_padded) - n_fft) // hop_length
    n_stft = n_fft // 2 + 1  # 201

    # Power spectrogram
    power_spec = np.zeros((n_frames, n_stft), dtype=np.float64)
    for i in range(n_frames):
        offset = i * hop_length
        frame = audio_padded[offset:offset + n_fft].astype(np.float64)
        frame *= window
        fft_out = np.fft.rfft(frame, n=n_fft)
        # spectral_magnitude(stft, power=1) = |stft|^2 (power spectrogram)
        power_spec[i] = np.abs(fft_out) ** 2

    # Mel filterbank (SpeechBrain triangular filters)
    # HTK mel scale: mel = 2595 * log10(1 + hz/700)
    def hz_to_mel(hz):
        return 2595.0 * np.log10(1.0 + hz / 700.0)

    def mel_to_hz(mel):
        return 700.0 * (10.0 ** (mel / 2595.0) - 1.0)

    # n_mels+2 points linearly spaced in mel domain
    mel_points = np.linspace(hz_to_mel(f_min), hz_to_mel(f_max), n_mels + 2)
    hz_points = mel_to_hz(mel_points)

    # Central frequencies and bands
    f_central = hz_points[1:-1]  # [n_mels]
    band = np.diff(hz_points)    # [n_mels+1]
    band = band[:-1]             # [n_mels] — left band width

    # All frequency bins
    all_freqs = np.linspace(0, sample_rate // 2, n_stft)  # [201]

    # Triangular filter matrix [n_stft, n_mels]
    # slope = (all_freqs - f_central) / band
    # left_side = slope + 1.0, right_side = -slope + 1.0
    # filter = max(0, min(left_side, right_side))
    fbank_matrix = np.zeros((n_stft, n_mels), dtype=np.float64)
    for m in range(n_mels):
        slope = (all_freqs - f_central[m]) / band[m]
        left_side = slope + 1.0
        right_side = -slope + 1.0
        fbank_matrix[:, m] = np.maximum(0.0, np.minimum(left_side, right_side))

    # Apply filterbank: [n_frames, n_stft] @ [n_stft, n_mels] -> [n_frames, n_mels]
    mel_spec = power_spec @ fbank_matrix

    # amplitude_to_DB: 10 * log10(clamp(x, min=1e-14)) - 10*log10(max(1e-14, 1e-14))
    # Then top_db=80 clipping
    amin = 1e-14
    ref_value = 1e-14
    top_db = 80.0
    db_multiplier = np.log10(max(amin, ref_value))

    log_mel = 10.0 * np.log10(np.maximum(mel_spec, amin))
    log_mel -= 10.0 * db_multiplier  # subtract reference

    # top_db clipping per sequence
    max_val = log_mel.max() - top_db
    log_mel = np.maximum(log_mel, max_val)

    # InputNormalization(norm_type=sentence, std_norm=False)
    # = subtract mean over time for each feature dim
    mean = log_mel.mean(axis=0, keepdims=True)
    log_mel -= mean

    return log_mel.astype(np.float32)


def test_with_ecapa_model(wav_path, ckpt_path):
    """End-to-end test: numpy fbank -> ECAPA-TDNN model -> embedding.
    Compare with the old C++ fbank to show the difference."""
    import torch
    sys.path.insert(0, os.path.dirname(__file__))
    from debug_ecapa_pytorch import ECAPA_TDNN, load_speechbrain_ckpt

    model = ECAPA_TDNN(n_mels=80, channels=1024, emb_dim=192, res2_scale=8)
    model = load_speechbrain_ckpt(model, ckpt_path)
    model.eval()

    # New SpeechBrain-compatible fbank
    np_feats = numpy_speechbrain_fbank(wav_path)
    print(f"  SB-compat fbank: shape={np_feats.shape}, mean={np_feats.mean():.4f}, std={np_feats.std():.4f}")

    fbank_t = torch.from_numpy(np_feats).unsqueeze(0)  # [1, T, 80]
    with torch.no_grad():
        emb, _ = model(fbank_t, debug=False)
    emb_np = emb.squeeze(0).numpy()
    emb_np = emb_np / (np.linalg.norm(emb_np) + 1e-10)
    return emb_np


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--wav", type=str, default=None)
    parser.add_argument("--ckpt", type=str, default=None)
    parser.add_argument("--test-dir", type=str, default=None)
    args = parser.parse_args()

    script_dir = os.path.dirname(os.path.abspath(__file__))
    repo_root = os.path.join(script_dir, "..", "..")

    if args.ckpt is None:
        args.ckpt = os.path.join(repo_root, "ecapa-raw", "embedding_model.ckpt")
    if args.test_dir is None:
        args.test_dir = os.path.join(script_dir, "..", "test", "real_speech")

    if args.wav:
        # Single file mode: just show fbank stats
        np_feats = numpy_speechbrain_fbank(args.wav)
        print(f"SB-compat fbank: shape={np_feats.shape}, mean={np_feats.mean():.6f}, std={np_feats.std():.6f}")
        print(f"  [:3,:5] = {np_feats[:3,:5]}")
        return

    # Multi-file speaker verification test
    if not os.path.exists(args.ckpt):
        print(f"Checkpoint not found: {args.ckpt}")
        sys.exit(1)
    if not os.path.isdir(args.test_dir):
        print(f"Test dir not found: {args.test_dir}")
        sys.exit(1)

    wav_files = sorted([f for f in os.listdir(args.test_dir) if f.endswith('.wav')])
    if not wav_files:
        print("No WAV files found")
        sys.exit(1)

    print(f"Computing embeddings with SpeechBrain-compatible fbank...")
    print(f"Checkpoint: {args.ckpt}")
    print(f"Test dir: {args.test_dir}")
    print()

    embeddings = {}
    for wav_name in wav_files:
        wav_path = os.path.join(args.test_dir, wav_name)
        print(f"  {wav_name}...", end=" ", flush=True)
        emb = test_with_ecapa_model(wav_path, args.ckpt)
        embeddings[wav_name] = emb
        print(f"done")

    # Cosine similarities
    names = list(embeddings.keys())
    print(f"\n{'='*70}")
    print("Cosine Similarities (SpeechBrain-compatible fbank):")
    print(f"{'='*70}")
    for i in range(len(names)):
        for j in range(i+1, len(names)):
            cos = np.dot(embeddings[names[i]], embeddings[names[j]])
            # Determine if same speaker
            spk_i = names[i].rsplit('_', 1)[0]
            spk_j = names[j].rsplit('_', 1)[0]
            tag = "SAME" if spk_i == spk_j else "DIFF"
            if tag == "SAME" or cos > 0.3:
                print(f"  {names[i]:35s} vs {names[j]:35s} => {cos:+.4f} [{tag}]")


if __name__ == "__main__":
    main()

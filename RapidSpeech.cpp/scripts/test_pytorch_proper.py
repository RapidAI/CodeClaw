#!/usr/bin/env python3
"""
Test ECAPA-TDNN with proper SpeechBrain-compatible fbank features.
SpeechBrain uses torchaudio.compliance.kaldi.fbank() internally.
"""
import os, sys, torch, numpy as np
import torch.nn.functional as F

# Import our model
sys.path.insert(0, os.path.dirname(__file__))
from debug_ecapa_pytorch import ECAPA_TDNN, load_speechbrain_ckpt

def load_wav(path, target_sr=16000):
    """Load WAV file as float32 tensor [1, T]."""
    try:
        import soundfile as sf
        audio, sr = sf.read(path)
        audio = torch.from_numpy(audio.astype(np.float32))
        if audio.dim() > 1:
            audio = audio.mean(dim=1)
        audio = audio.unsqueeze(0)  # [1, T]
        if sr != target_sr:
            import torchaudio
            audio = torchaudio.functional.resample(audio, sr, target_sr)
        return audio
    except Exception as e:
        print(f"  Error loading {path}: {e}")
        return None

def compute_fbank_kaldi(waveform, sr=16000, n_mels=80):
    """Compute fbank using torchaudio kaldi-compatible method (what SpeechBrain uses)."""
    try:
        import torchaudio
        # SpeechBrain default: 25ms frame, 10ms hop, 80 mels
        feats = torchaudio.compliance.kaldi.fbank(
            waveform, num_mel_bins=n_mels, sample_frequency=sr,
            frame_length=25.0, frame_shift=10.0,
            window_type='hamming', use_energy=False,
        )
        return feats  # [T, n_mels]
    except Exception as e:
        print(f"  torchaudio fbank failed: {e}")
        return None

def main():
    ckpt_path = os.path.join(os.path.dirname(__file__), "..", "..", "ecapa-raw", "embedding_model.ckpt")
    test_dir = os.path.join(os.path.dirname(__file__), "..", "test", "real_speech")
    
    model = ECAPA_TDNN(n_mels=80, channels=1024, emb_dim=192, res2_scale=8)
    model = load_speechbrain_ckpt(model, ckpt_path)
    model.eval()
    
    wav_files = sorted([f for f in os.listdir(test_dir) if f.endswith('.wav')])
    
    embeddings = {}
    for wav_name in wav_files:
        wav_path = os.path.join(test_dir, wav_name)
        waveform = load_wav(wav_path)
        if waveform is None:
            continue
        
        feats = compute_fbank_kaldi(waveform)
        if feats is None:
            continue
        
        feats = feats.unsqueeze(0)  # [1, T, 80]
        
        with torch.no_grad():
            emb, _ = model(feats, debug=False)
        
        emb_np = emb.squeeze(0).numpy()
        emb_np = emb_np / (np.linalg.norm(emb_np) + 1e-10)
        embeddings[wav_name] = emb_np
        print(f"{wav_name:35s} emb_norm={np.linalg.norm(emb.numpy()):.1f}  emb[:5]={emb_np[:5]}")
    
    # Compute similarities
    names = sorted(embeddings.keys())
    print(f"\n{'='*70}")
    print("Same-speaker pairs:")
    same_scores = []
    for i in range(len(names)):
        for j in range(i+1, len(names)):
            spk_i = names[i].rsplit('_', 1)[0]
            spk_j = names[j].rsplit('_', 1)[0]
            if spk_i == spk_j:
                cos = np.dot(embeddings[names[i]], embeddings[names[j]])
                same_scores.append(cos)
                print(f"  {names[i]:30s} vs {names[j]:30s} => {cos:.4f}")
    
    print(f"\nDifferent-speaker pairs (sample):")
    diff_scores = []
    for i in range(len(names)):
        for j in range(i+1, len(names)):
            spk_i = names[i].rsplit('_', 1)[0]
            spk_j = names[j].rsplit('_', 1)[0]
            if spk_i != spk_j:
                cos = np.dot(embeddings[names[i]], embeddings[names[j]])
                diff_scores.append(cos)
    
    # Show a sample of diff pairs
    diff_pairs = []
    for i in range(len(names)):
        for j in range(i+1, len(names)):
            spk_i = names[i].rsplit('_', 1)[0]
            spk_j = names[j].rsplit('_', 1)[0]
            if spk_i != spk_j:
                cos = np.dot(embeddings[names[i]], embeddings[names[j]])
                diff_pairs.append((names[i], names[j], cos))
    
    diff_pairs.sort(key=lambda x: x[2])
    for a, b, c in diff_pairs[:10]:
        print(f"  {a:30s} vs {b:30s} => {c:.4f}")
    print(f"  ...")
    for a, b, c in diff_pairs[-5:]:
        print(f"  {a:30s} vs {b:30s} => {c:.4f}")
    
    print(f"\n{'='*70}")
    print(f"Same speaker:  n={len(same_scores)}, min={min(same_scores):.4f}, max={max(same_scores):.4f}, avg={np.mean(same_scores):.4f}")
    print(f"Diff speaker:  n={len(diff_scores)}, min={min(diff_scores):.4f}, max={max(diff_scores):.4f}, avg={np.mean(diff_scores):.4f}")
    gap = min(same_scores) - max(diff_scores)
    print(f"Gap: {gap:.4f}")

if __name__ == "__main__":
    main()

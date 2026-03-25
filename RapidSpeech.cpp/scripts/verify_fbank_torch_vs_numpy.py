#!/usr/bin/env python3
"""Quick verification: compare torch.stft-based fbank with numpy reimplementation."""
import numpy as np
import torch
import sys, os

def torch_fbank(audio_np, n_mels=80, sr=16000):
    """Compute fbank using torch.stft (ground truth for SpeechBrain pipeline)."""
    audio = torch.from_numpy(audio_np).float()
    n_fft = 400
    hop = 160
    win = torch.hamming_window(n_fft)
    
    # torch.stft with center=True, pad_mode='constant' (zeros)
    stft = torch.stft(audio, n_fft, hop, n_fft, win, center=True,
                       pad_mode='constant', normalized=False, onesided=True,
                       return_complex=False)
    # stft: [n_stft, n_frames, 2]
    power = stft[..., 0]**2 + stft[..., 1]**2  # [201, T]
    power = power.T  # [T, 201]
    
    # SpeechBrain spectral_magnitude(power=1) = magnitude^2 = power spectrum
    # (spectral_magnitude computes stft.pow(2).sum(-1) which is already power)
    
    # Mel filterbank (SpeechBrain style)
    def hz2mel(hz): return 2595.0 * np.log10(1.0 + hz / 700.0)
    def mel2hz(mel): return 700.0 * (10.0 ** (mel / 2595.0) - 1.0)
    
    mel_pts = np.linspace(hz2mel(0), hz2mel(8000), n_mels + 2)
    hz_pts = mel2hz(mel_pts)
    f_central = hz_pts[1:-1]
    band = np.diff(hz_pts)[:-1]
    all_freqs = np.linspace(0, sr // 2, 201)
    
    fbank_mat = np.zeros((201, n_mels), dtype=np.float32)
    for m in range(n_mels):
        slope = (all_freqs - f_central[m]) / band[m]
        fbank_mat[:, m] = np.maximum(0, np.minimum(slope + 1, -slope + 1))
    
    fbank_mat_t = torch.from_numpy(fbank_mat)
    mel = torch.matmul(power, fbank_mat_t)
    
    # amplitude_to_DB
    amin = 1e-14
    log_mel = 10.0 * torch.log10(torch.clamp(mel, min=amin))
    log_mel -= 10.0 * np.log10(max(amin, amin))
    max_val = log_mel.max() - 80.0
    log_mel = torch.maximum(log_mel, max_val)
    
    # sentence mean norm
    log_mel -= log_mel.mean(dim=0, keepdim=True)
    return log_mel.numpy()

def numpy_fbank(audio_np, n_mels=80, sr=16000):
    """Pure numpy (same as verify_speechbrain_fbank.py)."""
    n_fft = 400; hop = 160
    window = 0.54 - 0.46 * np.cos(2 * np.pi * np.arange(n_fft) / n_fft)
    pad_len = n_fft // 2
    audio_padded = np.pad(audio_np, (pad_len, pad_len), mode='constant')
    n_frames = 1 + (len(audio_padded) - n_fft) // hop
    
    power_spec = np.zeros((n_frames, 201), dtype=np.float64)
    for i in range(n_frames):
        frame = audio_padded[i*hop:i*hop+n_fft].astype(np.float64) * window
        fft_out = np.fft.rfft(frame, n=n_fft)
        power_spec[i] = np.abs(fft_out) ** 2
    
    def hz2mel(hz): return 2595.0 * np.log10(1.0 + hz / 700.0)
    def mel2hz(mel): return 700.0 * (10.0 ** (mel / 2595.0) - 1.0)
    mel_pts = np.linspace(hz2mel(0), hz2mel(8000), n_mels + 2)
    hz_pts = mel2hz(mel_pts)
    f_central = hz_pts[1:-1]; band = np.diff(hz_pts)[:-1]
    all_freqs = np.linspace(0, sr // 2, 201)
    fbank_mat = np.zeros((201, n_mels), dtype=np.float64)
    for m in range(n_mels):
        slope = (all_freqs - f_central[m]) / band[m]
        fbank_mat[:, m] = np.maximum(0, np.minimum(slope + 1, -slope + 1))
    
    mel = power_spec @ fbank_mat
    amin = 1e-14
    log_mel = 10.0 * np.log10(np.maximum(mel, amin))
    log_mel -= 10.0 * np.log10(max(amin, amin))
    max_val = log_mel.max() - 80.0
    log_mel = np.maximum(log_mel, max_val)
    log_mel -= log_mel.mean(axis=0, keepdims=True)
    return log_mel.astype(np.float32)

if __name__ == "__main__":
    import soundfile as sf
    wav = sys.argv[1] if len(sys.argv) > 1 else "RapidSpeech.cpp/test/real_speech/en_male_guy_0.wav"
    audio, sr = sf.read(wav)
    if len(audio.shape) > 1: audio = audio[:, 0]
    audio = audio.astype(np.float32)
    
    t_feats = torch_fbank(audio)
    n_feats = numpy_fbank(audio)
    
    print(f"Torch:  shape={t_feats.shape}, mean={t_feats.mean():.6f}, std={t_feats.std():.6f}")
    print(f"Numpy:  shape={n_feats.shape}, mean={n_feats.mean():.6f}, std={n_feats.std():.6f}")
    
    diff = np.abs(t_feats - n_feats)
    cos = np.dot(t_feats.flatten(), n_feats.flatten()) / (
        np.linalg.norm(t_feats.flatten()) * np.linalg.norm(n_feats.flatten()) + 1e-12)
    print(f"max_diff={diff.max():.8f}, mean_diff={diff.mean():.8f}, cos_sim={cos:.8f}")

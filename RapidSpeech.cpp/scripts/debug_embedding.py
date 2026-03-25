#!/usr/bin/env python3
"""Debug: compare C++ ECAPA-TDNN embeddings with PyTorch reference.
Loads the same checkpoint used for GGUF conversion and runs forward pass."""

import os
import sys
import json
import subprocess
import numpy as np
import torch
import soundfile as sf

SERVER = "http://localhost:8090"

def get_cpp_embedding(wav_path):
    r = subprocess.run(
        ["curl.exe", "-s", "-X", "POST", f"{SERVER}/v1/speaker-embed",
         "-F", f"file=@{wav_path}"],
        capture_output=True, text=True, timeout=60
    )
    d = json.loads(r.stdout)
    return np.array(d["embedding"])

def load_wav(wav_path, target_sr=16000):
    """Load WAV using soundfile."""
    data, sr = sf.read(wav_path, dtype='float32')
    if len(data.shape) > 1:
        data = data[:, 0]
    if sr != target_sr:
        import torchaudio
        t = torch.from_numpy(data).unsqueeze(0)
        t = torchaudio.functional.resample(t, sr, target_sr)
        data = t.squeeze(0).numpy()
    return data, target_sr

def compute_fbank_pytorch(wav_path, n_mels=80, n_fft=400, hop=160, sr=16000):
    """Compute fbank features."""
    data, sr = load_wav(wav_path, sr)
    waveform = torch.from_numpy(data).unsqueeze(0)  # [1, T]
    
    # Use torchaudio kaldi fbank
    import torchaudio
    fbank = torchaudio.compliance.kaldi.fbank(
        waveform, num_mel_bins=n_mels, sample_frequency=sr,
        frame_length=25.0, frame_shift=10.0,
        window_type='hamming', use_energy=False
    )
    return fbank  # [T, n_mels]

def load_ecapa_and_forward(ckpt_path, fbank):
    """Load SpeechBrain ECAPA-TDNN checkpoint and run forward pass.
    This is a minimal reimplementation of the forward pass."""
    ckpt = torch.load(ckpt_path, map_location='cpu')
    
    # The checkpoint has keys like 'embedding_model.blocks.0.conv.weight' etc.
    # Let's just use the full model
    # First, let's see what keys are in the checkpoint
    print(f"  Checkpoint keys ({len(ckpt)} total):")
    for k in sorted(ckpt.keys())[:20]:
        print(f"    {k}: {ckpt[k].shape}")
    if len(ckpt) > 20:
        print(f"    ... and {len(ckpt)-20} more")
    
    return None  # We'll implement the full forward pass if needed

def main():
    wavs = [
        "RapidSpeech.cpp/test/real_speech/zh_male_yunjian_0.wav",
        "RapidSpeech.cpp/test/real_speech/zh_female_xiaoxiao_0.wav",
        "RapidSpeech.cpp/test/real_speech/en_male_guy_0.wav",
    ]
    
    # Step 1: Check fbank features
    print("=== Fbank Features ===")
    for w in wavs:
        name = os.path.basename(w).replace(".wav", "")
        fbank = compute_fbank_pytorch(w)
        print(f"  {name}: shape={fbank.shape} mean={fbank.mean():.4f} std={fbank.std():.4f} min={fbank.min():.4f} max={fbank.max():.4f}")
    
    # Step 2: Check C++ embeddings
    print("\n=== C++ Embeddings ===")
    cpp_embs = {}
    for w in wavs:
        name = os.path.basename(w).replace(".wav", "")
        e = get_cpp_embedding(w)
        cpp_embs[name] = e
        print(f"  {name}: norm={np.linalg.norm(e):.4f} mean={e.mean():.6f} std={e.std():.6f}")
        print(f"    first10: {e[:10]}")
    
    # Step 3: Check variance of embeddings
    print("\n=== Embedding Variance Analysis ===")
    all_embs = np.stack(list(cpp_embs.values()))
    per_dim_var = np.var(all_embs, axis=0)
    print(f"  Per-dimension variance: mean={per_dim_var.mean():.8f} max={per_dim_var.max():.8f}")
    print(f"  Dims with var > 1e-5: {np.sum(per_dim_var > 1e-5)}/{len(per_dim_var)}")
    print(f"  Dims with var > 1e-4: {np.sum(per_dim_var > 1e-4)}/{len(per_dim_var)}")
    
    # Step 4: Cosine similarities
    print("\n=== Cosine Similarities ===")
    names = sorted(cpp_embs.keys())
    for i in range(len(names)):
        for j in range(i+1, len(names)):
            cos = np.dot(cpp_embs[names[i]], cpp_embs[names[j]])
            print(f"  {names[i]} vs {names[j]}: {cos:.6f}")
    
    # Step 5: Check if embeddings are dominated by a few dimensions
    print("\n=== Top Contributing Dimensions ===")
    ref = list(cpp_embs.values())[0]
    top_idx = np.argsort(np.abs(ref))[::-1][:10]
    print(f"  Top 10 dims by magnitude: {top_idx}")
    print(f"  Values: {ref[top_idx]}")
    
    # Check if the issue is that all embeddings point in the same direction
    mean_emb = all_embs.mean(axis=0)
    mean_emb_norm = mean_emb / np.linalg.norm(mean_emb)
    print(f"\n  Mean embedding norm: {np.linalg.norm(mean_emb):.4f}")
    for name, emb in cpp_embs.items():
        cos_to_mean = np.dot(emb, mean_emb_norm)
        print(f"  {name} cosine to mean: {cos_to_mean:.6f}")

if __name__ == "__main__":
    main()

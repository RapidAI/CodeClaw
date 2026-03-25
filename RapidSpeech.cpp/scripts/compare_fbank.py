#!/usr/bin/env python3
"""Compare our custom fbank with torchaudio kaldi fbank to find the mismatch."""
import os, sys, torch, numpy as np

sys.path.insert(0, os.path.dirname(__file__))
from debug_ecapa_pytorch import compute_fbank_pytorch

test_dir = os.path.join(os.path.dirname(__file__), "..", "test", "real_speech")
wav_path = os.path.join(test_dir, "en_female_jenny_0.wav")

# Load audio
import soundfile as sf
audio, sr = sf.read(wav_path)
if len(audio.shape) > 1:
    audio = audio.mean(axis=1)
audio = audio.astype(np.float32)
print(f"Audio: {len(audio)} samples, sr={sr}")

# Our custom fbank
our_fbank = compute_fbank_pytorch(wav_path)
print(f"\nOur fbank:     shape={our_fbank.shape}, mean={our_fbank.mean():.4f}, std={our_fbank.std():.4f}")
print(f"  min={our_fbank.min():.4f}, max={our_fbank.max():.4f}")
print(f"  per-mel mean range: [{our_fbank.mean(axis=0).min():.4f}, {our_fbank.mean(axis=0).max():.4f}]")

# Torchaudio kaldi fbank
import torchaudio
waveform = torch.from_numpy(audio).unsqueeze(0)
kaldi_fbank = torchaudio.compliance.kaldi.fbank(
    waveform, num_mel_bins=80, sample_frequency=sr,
    frame_length=25.0, frame_shift=10.0,
    window_type='hamming', use_energy=False,
).numpy()
print(f"\nKaldi fbank:   shape={kaldi_fbank.shape}, mean={kaldi_fbank.mean():.4f}, std={kaldi_fbank.std():.4f}")
print(f"  min={kaldi_fbank.min():.4f}, max={kaldi_fbank.max():.4f}")
print(f"  per-mel mean range: [{kaldi_fbank.mean(axis=0).min():.4f}, {kaldi_fbank.mean(axis=0).max():.4f}]")

# Compare frame by frame
min_frames = min(our_fbank.shape[0], kaldi_fbank.shape[0])
diff = our_fbank[:min_frames] - kaldi_fbank[:min_frames]
print(f"\nDifference (our - kaldi):")
print(f"  mean={diff.mean():.4f}, std={diff.std():.4f}")
print(f"  abs mean={np.abs(diff).mean():.4f}")
print(f"  max abs={np.abs(diff).max():.4f}")

# Show first frame comparison
print(f"\nFrame 0 comparison (first 10 mels):")
print(f"  Our:   {our_fbank[0, :10]}")
print(f"  Kaldi: {kaldi_fbank[0, :10]}")
print(f"  Diff:  {diff[0, :10]}")

# The key question: does the BN running_mean match kaldi fbank stats?
# blocks.0 BN running_mean has mean=16.1
# If kaldi fbank has similar per-channel means, the BN will work correctly
print(f"\nKaldi fbank per-mel means (first 10):")
print(f"  {kaldi_fbank.mean(axis=0)[:10]}")
print(f"\nOur fbank per-mel means (first 10):")
print(f"  {our_fbank.mean(axis=0)[:10]}")

# Check: after Conv1d(80, 1024, 5), what's the expected output range?
# The BN running_mean=16.1 suggests the conv output has mean ~16
# This means the fbank values need to be in a specific range
print(f"\nScale analysis:")
print(f"  Kaldi fbank overall mean: {kaldi_fbank.mean():.4f}")
print(f"  Our fbank overall mean:   {our_fbank.mean():.4f}")
print(f"  Ratio: {kaldi_fbank.mean() / our_fbank.mean():.4f}")

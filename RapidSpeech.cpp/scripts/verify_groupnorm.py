"""Verify GroupNorm(1, dim) implementation by comparing Python vs C++ frontend outputs.
Specifically tests that the normalization is done over the entire [C, T] plane (not per-timestep).
"""
import numpy as np
import torch
import torch.nn.functional as F

# Create a small synthetic test: [1, C=4, T=3]
# With known values so we can verify the normalization manually.
C, T = 4, 3
x = torch.tensor([[[1.0, 2.0, 3.0],
                    [4.0, 5.0, 6.0],
                    [7.0, 8.0, 9.0],
                    [10.0, 11.0, 12.0]]])  # [1, 4, 3]

weight = torch.ones(C)
bias = torch.zeros(C)

# PyTorch GroupNorm(1, C) — normalizes over entire [C, T] plane
result = F.group_norm(x, 1, weight, bias, eps=1e-5)
print(f"Input shape: {x.shape}")
print(f"Input:\n{x[0]}")
print(f"\nGroupNorm(1, {C}) output:\n{result[0]}")

# Manual computation: mean and var over all C*T elements
all_vals = x[0].flatten()
mean = all_vals.mean()
var = all_vals.var(unbiased=False)
print(f"\nManual: mean={mean.item():.6f}, var={var.item():.6f}")
manual_result = (x[0] - mean) / torch.sqrt(var + 1e-5)
print(f"Manual result:\n{manual_result}")
print(f"Match: {torch.allclose(result[0], manual_result, atol=1e-5)}")

# Now verify what the OLD (wrong) implementation would produce:
# Old: transpose -> ggml_norm (per-column norm) -> transpose
# ggml_norm normalizes over ne[0]. After transpose, ne[0]=C, ne[1]=T
# So it normalizes each column (time step) independently over channels.
print("\n--- OLD (wrong) per-timestep normalization ---")
for t in range(T):
    col = x[0, :, t]
    col_mean = col.mean()
    col_var = col.var(unbiased=False)
    col_normed = (col - col_mean) / torch.sqrt(col_var + 1e-5)
    print(f"  t={t}: mean={col_mean.item():.2f}, var={col_var.item():.2f}, normed={col_normed.tolist()}")

print("\n--- NEW (correct) whole-plane normalization ---")
print(f"  mean={mean.item():.2f}, var={var.item():.2f}")
print(f"  All values normalized with same mean/var")

# Now test with the actual moonshine frontend weights
print("\n" + "="*60)
print("Testing with actual moonshine-tiny frontend...")
print("="*60)

from pathlib import Path
from safetensors import safe_open
import wave

model_dir = Path("models/moonshine-tiny")
wav_path = "test/real_speech/en_female_jenny_0.wav"

if model_dir.exists() and Path(wav_path).exists():
    # Load audio
    with wave.open(wav_path, "rb") as wf:
        frames = wf.readframes(wf.getnframes())
        pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    pcm_t = torch.from_numpy(pcm).unsqueeze(0)

    # Load weights
    weights = {}
    with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
        for name in f.keys():
            weights[name] = f.get_tensor(name).float()

    conv1_w = weights["model.encoder.conv1.weight"]
    conv2_w = weights["model.encoder.conv2.weight"]
    conv2_b = weights["model.encoder.conv2.bias"]
    conv3_w = weights["model.encoder.conv3.weight"]
    conv3_b = weights["model.encoder.conv3.bias"]
    gn_w = weights["model.encoder.groupnorm.weight"]
    gn_b = weights["model.encoder.groupnorm.bias"]

    # Step 1: conv1 + tanh
    x = F.conv1d(pcm_t.unsqueeze(1), conv1_w, stride=64)
    x = torch.tanh(x)
    print(f"\nAfter conv1+tanh: shape={x.shape}")
    print(f"  x[0,:5,0] = {x[0,:5,0].tolist()}")

    # Step 2: GroupNorm(1, 288)
    x_gn = F.group_norm(x, 1, gn_w, gn_b)
    print(f"\nAfter GroupNorm(1, 288):")
    print(f"  shape={x_gn.shape}")
    print(f"  x_gn[0,:5,0] = {x_gn[0,:5,0].tolist()}")

    # Compute the global mean/var that GroupNorm uses
    gn_mean = x[0].mean()
    gn_var = x[0].var(unbiased=False)
    print(f"  GroupNorm mean={gn_mean.item():.6f}, var={gn_var.item():.6f}")

    # What the OLD per-timestep norm would give for frame 0:
    frame0 = x[0, :, 0]  # [288]
    old_mean = frame0.mean()
    old_var = frame0.var(unbiased=False)
    old_normed = (frame0 - old_mean) / torch.sqrt(old_var + 1e-5)
    old_result = old_normed * gn_w + gn_b
    print(f"\n  OLD per-timestep frame0: mean={old_mean.item():.6f}, var={old_var.item():.6f}")
    print(f"  OLD result[:5] = {old_result[:5].tolist()}")
    print(f"  NEW result[:5] = {x_gn[0,:5,0].tolist()}")
    print(f"  Difference[:5] = {(old_result[:5] - x_gn[0,:5,0]).tolist()}")

    # Continue full pipeline
    x_gn2 = F.conv1d(x_gn, conv2_w, conv2_b, stride=3)
    x_gn2 = F.gelu(x_gn2)
    x_gn2 = F.conv1d(x_gn2, conv3_w, conv3_b, stride=2)
    x_gn2 = F.gelu(x_gn2)
    x_gn2 = x_gn2.permute(0, 2, 1)  # [1, seq, dim]
    print(f"\nFrontend output: shape={x_gn2.shape}")
    print(f"  first 5: {x_gn2[0, 0, :5].tolist()}")
else:
    print("Model or audio files not found, skipping real test.")

"""Dump frontend intermediate values at each step for comparison with C++ debug output."""
import numpy as np
import torch
import torch.nn.functional as F
from pathlib import Path
from safetensors import safe_open
import wave

model_dir = Path("models/moonshine-tiny")
wav_path = "test/real_speech/en_female_jenny_0.wav"

with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
pcm_t = torch.from_numpy(pcm).unsqueeze(0)

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

# conv1 + tanh
# PyTorch conv1d: input [B, C_in, L], kernel [C_out, C_in, K]
x_raw = F.conv1d(pcm_t.unsqueeze(1), conv1_w, stride=64)  # [1, 288, 1103]
# Print raw conv1 output (before tanh)
nt = x_raw.shape[2]
nc = x_raw.shape[1]
mid = nt // 2
print(f"conv1_raw: shape=[{nt},{nc}] (ggml order)")
print(f"  frame{mid}_ch0-4: {x_raw[0, 0, mid].item():.6f} {x_raw[0, 1, mid].item():.6f} {x_raw[0, 2, mid].item():.6f} {x_raw[0, 3, mid].item():.6f} {x_raw[0, 4, mid].item():.6f}")
print(f"  frame10_ch0-4: {x_raw[0, 0, 10].item():.6f} {x_raw[0, 1, 10].item():.6f} {x_raw[0, 2, 10].item():.6f} {x_raw[0, 3, 10].item():.6f} {x_raw[0, 4, 10].item():.6f}")
print(f"  min={x_raw.min().item():.6f} max={x_raw.max().item():.6f}")

x = torch.tanh(x_raw)

# In ggml column-major: tensor is [time=1103, channels=288]
# C++ reads frame at mid=1103/2=551, channels 0-4
# In PyTorch: x[0, ch, time]
nt = x.shape[2]  # 1103
nc = x.shape[1]  # 288
mid = nt // 2
print(f"conv1_tanh: shape=[{nt},{nc}] (ggml order)")
print(f"  frame{mid}_ch0-4: {x[0, 0, mid].item():.6f} {x[0, 1, mid].item():.6f} {x[0, 2, mid].item():.6f} {x[0, 3, mid].item():.6f} {x[0, 4, mid].item():.6f}")
mean = x.mean().item()
var_val = (x**2).mean().item() - mean**2
print(f"  mean={mean:.6f} var={var_val:.6f}")

# GroupNorm(1, 288)
x_gn = F.group_norm(x, 1, gn_w, gn_b)
nt2 = x_gn.shape[2]
nc2 = x_gn.shape[1]
mid2 = nt2 // 2
print(f"\ngroupnorm: shape=[{nt2},{nc2}] (ggml order)")
print(f"  frame{mid2}_ch0-4: {x_gn[0, 0, mid2].item():.6f} {x_gn[0, 1, mid2].item():.6f} {x_gn[0, 2, mid2].item():.6f} {x_gn[0, 3, mid2].item():.6f} {x_gn[0, 4, mid2].item():.6f}")
mean_gn = x_gn.mean().item()
var_gn = (x_gn**2).mean().item() - mean_gn**2
print(f"  mean={mean_gn:.6f} var={var_gn:.6f}")

# conv2 + gelu
x2 = F.conv1d(x_gn, conv2_w, conv2_b, stride=3)
x2 = F.gelu(x2)

# conv3 + gelu
x3 = F.conv1d(x2, conv3_w, conv3_b, stride=2)
x3 = F.gelu(x3)
nt3 = x3.shape[2]
nc3 = x3.shape[1]
print(f"\npre_transpose: shape=[{nt3},{nc3}] (ggml order)")
print(f"  frame0_ch0-4: {x3[0, 0, 0].item():.6f} {x3[0, 1, 0].item():.6f} {x3[0, 2, 0].item():.6f} {x3[0, 3, 0].item():.6f} {x3[0, 4, 0].item():.6f}")

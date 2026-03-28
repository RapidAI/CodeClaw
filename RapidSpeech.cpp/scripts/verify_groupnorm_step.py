"""Step-by-step comparison: dump conv1+tanh output and groupnorm output separately.
This helps isolate whether the remaining encoder divergence is from conv or groupnorm.
"""
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

# Step 1: conv1 + tanh
x = F.conv1d(pcm_t.unsqueeze(1), conv1_w, stride=64)
x = torch.tanh(x)
# x shape: [1, 288, 1103]
# In ggml column-major, this is stored as [time=1103, channels=288]
# C++ debug prints "pre_transpose" which is AFTER conv3+gelu, not after conv1

# Let's trace the full frontend
print("=== Python Frontend Step-by-Step ===")
print(f"After conv1+tanh: shape={x.shape}")
# Print in ggml order: x[0, ch, time] -> ggml [time, ch]
# frame0 = x[0, :, 0] (all channels at time=0)
print(f"  frame0 ch0-4: {x[0, :5, 0].tolist()}")
print(f"  frame1 ch0-4: {x[0, :5, 1].tolist()}")

# Step 2: GroupNorm
x_gn = F.group_norm(x, 1, gn_w, gn_b)
print(f"\nAfter GroupNorm(1, 288): shape={x_gn.shape}")
print(f"  frame0 ch0-4: {x_gn[0, :5, 0].tolist()}")
print(f"  frame1 ch0-4: {x_gn[0, :5, 1].tolist()}")

# Step 3: conv2 + gelu
x2 = F.conv1d(x_gn, conv2_w, conv2_b, stride=3)
x2 = F.gelu(x2)
print(f"\nAfter conv2+gelu: shape={x2.shape}")

# Step 4: conv3 + gelu
x3 = F.conv1d(x2, conv3_w, conv3_b, stride=2)
x3 = F.gelu(x3)
print(f"\nAfter conv3+gelu: shape={x3.shape}")
# This is what C++ calls "pre_transpose" — shape [1, 288, 182]
# In ggml: [time=182, channels=288]
# C++ prints frame0_ch0-4 which reads x3[0, 0:5, 0] in PyTorch order
print(f"  frame0 ch0-4 (PyTorch [B,C,T]): {x3[0, :5, 0].tolist()}")

# After transpose: [1, 182, 288]
x_t = x3.permute(0, 2, 1)
print(f"\nAfter transpose: shape={x_t.shape}")
print(f"  frame0 first5: {x_t[0, 0, :5].tolist()}")

# C++ output for comparison:
print("\n=== C++ values (from log) ===")
print("  pre_transpose frame0_ch0-4: 1.250000 1.096680 0.814941 0.549316 0.708984")
print("  post_transpose frame0_first5: 1.250000 1.096680 0.814941 0.549316 0.708984")
print("  enc_out[0,:5]: 0.057651 0.051557 0.232158 0.163887 0.053771")

# Compute differences
cpp_frontend = [1.250000, 1.096680, 0.814941, 0.549316, 0.708984]
py_frontend = x_t[0, 0, :5].tolist()
print(f"\n=== Frontend output diff ===")
for i in range(5):
    diff = abs(cpp_frontend[i] - py_frontend[i])
    print(f"  ch{i}: py={py_frontend[i]:.6f} cpp={cpp_frontend[i]:.6f} diff={diff:.6f}")

cpp_enc = [0.057651, 0.051557, 0.232158, 0.163887, 0.053771]
py_enc = None

# Run full encoder to get Python enc output
def rms_norm(x, w, eps=1e-5):
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

dim = 288
n_heads = 8
head_dim = 36
rotary_dim = 32
scale = 1.0 / (head_dim ** 0.5)

def apply_rotary(x, rotary_dim, theta=10000.0):
    seq_len = x.shape[2]
    pos = torch.arange(seq_len, dtype=torch.float32)
    dim_pairs = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
    cos_a = torch.cos(angles).unsqueeze(0).unsqueeze(0)
    sin_a = torch.sin(angles).unsqueeze(0).unsqueeze(0)
    x_rot = x[..., :rotary_dim]
    x_pass = x[..., rotary_dim:]
    x1 = x_rot[..., :dim_pairs]
    x2 = x_rot[..., dim_pairs:]
    out1 = x1 * cos_a - x2 * sin_a
    out2 = x2 * cos_a + x1 * sin_a
    return torch.cat([out1, out2, x_pass], dim=-1)

x_full = x_t
for i in range(6):
    ln_w = weights[f"model.encoder.layers.{i}.input_layernorm.weight"]
    q_w = weights[f"model.encoder.layers.{i}.self_attn.q_proj.weight"]
    k_w = weights[f"model.encoder.layers.{i}.self_attn.k_proj.weight"]
    v_w = weights[f"model.encoder.layers.{i}.self_attn.v_proj.weight"]
    o_w = weights[f"model.encoder.layers.{i}.self_attn.o_proj.weight"]
    residual = x_full
    x_norm = rms_norm(x_full, ln_w)
    q = F.linear(x_norm, q_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
    k = F.linear(x_norm, k_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
    v = F.linear(x_norm, v_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
    q = apply_rotary(q, rotary_dim)
    k = apply_rotary(k, rotary_dim)
    scores = torch.matmul(q, k.transpose(-2, -1)) * scale
    attn = F.softmax(scores, dim=-1)
    out = torch.matmul(attn, v).permute(0, 2, 1, 3).reshape(1, -1, dim)
    out = F.linear(out, o_w)
    x_full = residual + out
    ff_ln_w = weights[f"model.encoder.layers.{i}.post_attention_layernorm.weight"]
    ff_up_w = weights[f"model.encoder.layers.{i}.mlp.fc1.weight"]
    ff_up_b = weights[f"model.encoder.layers.{i}.mlp.fc1.bias"]
    ff_down_w = weights[f"model.encoder.layers.{i}.mlp.fc2.weight"]
    ff_down_b = weights[f"model.encoder.layers.{i}.mlp.fc2.bias"]
    residual = x_full
    x_norm = rms_norm(x_full, ff_ln_w)
    x_ff = F.linear(x_norm, ff_up_w, ff_up_b)
    x_ff = F.gelu(x_ff)
    x_ff = F.linear(x_ff, ff_down_w, ff_down_b)
    x_full = residual + x_ff

final_ln_w = weights["model.encoder.layer_norm.weight"]
x_full = rms_norm(x_full, final_ln_w)
py_enc = x_full[0, 0, :5].tolist()

print(f"\n=== Encoder output diff ===")
for i in range(5):
    diff = abs(cpp_enc[i] - py_enc[i])
    print(f"  ch{i}: py={py_enc[i]:.6f} cpp={cpp_enc[i]:.6f} diff={diff:.6f}")

# Also check what Python decoder produces
embed_w = weights["model.decoder.embed_tokens.weight"]
logits_py = F.linear(x_full, embed_w)
top5 = torch.topk(logits_py[0, 0], 5)
print(f"\nPython logits top5 (using Python encoder): ids={top5.indices.tolist()}, vals={top5.values.tolist()}")

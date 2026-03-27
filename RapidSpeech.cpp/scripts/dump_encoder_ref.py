"""Dump moonshine-tiny encoder intermediate values using manual PyTorch forward pass.
Compare with C++ ggml implementation to find numerical divergence.
"""
import numpy as np
import json, wave
from pathlib import Path
from safetensors import safe_open
import torch
import torch.nn.functional as F

model_dir = Path("models/moonshine-tiny")
wav_path = "test/real_speech/en_female_jenny_0.wav"

# Load audio
with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
pcm_t = torch.from_numpy(pcm).unsqueeze(0)  # [1, samples]
print(f"Audio: {pcm_t.shape}")

# Load weights
weights = {}
with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys():
        weights[name] = f.get_tensor(name).float()

# Frontend
conv1_w = weights["model.encoder.conv1.weight"]  # [288, 1, 127]
conv2_w = weights["model.encoder.conv2.weight"]  # [576, 288, 7]
conv2_b = weights["model.encoder.conv2.bias"]
conv3_w = weights["model.encoder.conv3.weight"]  # [288, 576, 3]
conv3_b = weights["model.encoder.conv3.bias"]
gn_w = weights["model.encoder.groupnorm.weight"]
gn_b = weights["model.encoder.groupnorm.bias"]

# Conv frontend
x = F.conv1d(pcm_t.unsqueeze(1), conv1_w, stride=64)  # [1, 288, OL1]
x = torch.tanh(x)
x = F.group_norm(x, 1, gn_w, gn_b)
x = F.conv1d(x, conv2_w, conv2_b, stride=3)
x = F.gelu(x)
x = F.conv1d(x, conv3_w, conv3_b, stride=2)
x = F.gelu(x)
x = x.permute(0, 2, 1)  # [1, seq, dim]
print(f"Frontend output: {x.shape}")
print(f"  mean={x.mean().item():.6f}, std={x.std().item():.6f}")
print(f"  first 5: {x[0, 0, :5].tolist()}")

# Encoder layer 0
dim = 288
n_heads = 8
head_dim = 36

def rms_norm(x, w, eps=1e-5):
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

ln_w = weights["model.encoder.layers.0.input_layernorm.weight"]
q_w = weights["model.encoder.layers.0.self_attn.q_proj.weight"]
k_w = weights["model.encoder.layers.0.self_attn.k_proj.weight"]
v_w = weights["model.encoder.layers.0.self_attn.v_proj.weight"]
o_w = weights["model.encoder.layers.0.self_attn.o_proj.weight"]

residual = x
x_norm = rms_norm(x, ln_w)
print(f"\nLayer 0 after RMSNorm: mean={x_norm.mean().item():.6f}, std={x_norm.std().item():.6f}")

q = F.linear(x_norm, q_w)  # [1, seq, dim]
k = F.linear(x_norm, k_w)
v = F.linear(x_norm, v_w)
print(f"Q: mean={q.mean().item():.6f}, std={q.std().item():.6f}")

# Reshape to [batch, n_heads, seq, head_dim]
seq = q.shape[1]
q = q.view(1, seq, n_heads, head_dim).permute(0, 2, 1, 3)
k = k.view(1, seq, n_heads, head_dim).permute(0, 2, 1, 3)
v = v.view(1, seq, n_heads, head_dim).permute(0, 2, 1, 3)

# RoPE (partial, 90% of head_dim)
rotary_dim = int(head_dim * 0.9)
rotary_dim -= rotary_dim % 2  # 32

def apply_rotary(x, rotary_dim, theta=10000.0):
    seq_len = x.shape[2]
    pos = torch.arange(seq_len, dtype=torch.float32)
    dim_pairs = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos.unsqueeze(1) * freqs.unsqueeze(0)  # [seq, dim_pairs]
    cos_a = torch.cos(angles)
    sin_a = torch.sin(angles)
    
    x_rot = x[..., :rotary_dim]
    x_pass = x[..., rotary_dim:]
    
    x1 = x_rot[..., :dim_pairs]
    x2 = x_rot[..., dim_pairs:]
    
    # Unsqueeze for broadcasting: cos_a [seq, dim_pairs] -> [1, 1, seq, dim_pairs]
    cos_a = cos_a.unsqueeze(0).unsqueeze(0)
    sin_a = sin_a.unsqueeze(0).unsqueeze(0)
    
    out1 = x1 * cos_a - x2 * sin_a
    out2 = x2 * cos_a + x1 * sin_a
    
    return torch.cat([out1, out2, x_pass], dim=-1)

q = apply_rotary(q, rotary_dim)
k = apply_rotary(k, rotary_dim)

# Attention
scale = 1.0 / (head_dim ** 0.5)
scores = torch.matmul(q, k.transpose(-2, -1)) * scale
attn = F.softmax(scores, dim=-1)
out = torch.matmul(attn, v)  # [1, n_heads, seq, head_dim]
out = out.permute(0, 2, 1, 3).reshape(1, seq, dim)  # [1, seq, dim]
out = F.linear(out, o_w)
x = residual + out

print(f"Layer 0 after self-attn: mean={x.mean().item():.6f}, std={x.std().item():.6f}")
print(f"  first 5: {x[0, 0, :5].tolist()}")

# FFN
ff_ln_w = weights["model.encoder.layers.0.post_attention_layernorm.weight"]
ff_up_w = weights["model.encoder.layers.0.mlp.fc1.weight"]
ff_up_b = weights["model.encoder.layers.0.mlp.fc1.bias"]
ff_down_w = weights["model.encoder.layers.0.mlp.fc2.weight"]
ff_down_b = weights["model.encoder.layers.0.mlp.fc2.bias"]

residual = x
x_norm = rms_norm(x, ff_ln_w)
x_ff = F.linear(x_norm, ff_up_w, ff_up_b)
x_ff = F.gelu(x_ff)
x_ff = F.linear(x_ff, ff_down_w, ff_down_b)
x = residual + x_ff

print(f"Layer 0 after FFN: mean={x.mean().item():.6f}, std={x.std().item():.6f}")
print(f"  first 5: {x[0, 0, :5].tolist()}")

# Run all 6 encoder layers
x_full = pcm_t.unsqueeze(1)
x_full = F.conv1d(x_full, conv1_w, stride=64)
x_full = torch.tanh(x_full)
x_full = F.group_norm(x_full, 1, gn_w, gn_b)
x_full = F.conv1d(x_full, conv2_w, conv2_b, stride=3)
x_full = F.gelu(x_full)
x_full = F.conv1d(x_full, conv3_w, conv3_b, stride=2)
x_full = F.gelu(x_full)
x_full = x_full.permute(0, 2, 1)

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

print(f"\nFinal encoder output: {x_full.shape}")
print(f"  mean={x_full.mean().item():.6f}, std={x_full.std().item():.6f}")
print(f"  first 5: {x_full[0, 0, :5].tolist()}")

# Now test decoder step 0 with BOS token
embed_w = weights["model.decoder.embed_tokens.weight"]  # [32768, 288]
bos_id = 1
tok_emb = embed_w[bos_id].unsqueeze(0).unsqueeze(0)  # [1, 1, 288]
print(f"\nBOS embedding: mean={tok_emb.mean().item():.6f}, first 5: {tok_emb[0,0,:5].tolist()}")

# Decoder layer 0 self-attention
d_ln_w = weights["model.decoder.layers.0.input_layernorm.weight"]
d_q_w = weights["model.decoder.layers.0.self_attn.q_proj.weight"]
d_k_w = weights["model.decoder.layers.0.self_attn.k_proj.weight"]
d_v_w = weights["model.decoder.layers.0.self_attn.v_proj.weight"]
d_o_w = weights["model.decoder.layers.0.self_attn.o_proj.weight"]

residual = tok_emb
x_norm = rms_norm(tok_emb, d_ln_w)
dq = F.linear(x_norm, d_q_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
dk = F.linear(x_norm, d_k_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
dv = F.linear(x_norm, d_v_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
dq = apply_rotary(dq, rotary_dim)
dk = apply_rotary(dk, rotary_dim)
scores = torch.matmul(dq, dk.transpose(-2, -1)) * scale
attn = F.softmax(scores, dim=-1)
out = torch.matmul(attn, dv).permute(0, 2, 1, 3).reshape(1, 1, dim)
out = F.linear(out, d_o_w)
dec_x = residual + out
print(f"Dec layer 0 after self-attn: mean={dec_x.mean().item():.6f}, first 5: {dec_x[0,0,:5].tolist()}")

# Cross-attention
c_ln_w = weights["model.decoder.layers.0.post_attention_layernorm.weight"]
c_q_w = weights["model.decoder.layers.0.encoder_attn.q_proj.weight"]
c_k_w = weights["model.decoder.layers.0.encoder_attn.k_proj.weight"]
c_v_w = weights["model.decoder.layers.0.encoder_attn.v_proj.weight"]
c_o_w = weights["model.decoder.layers.0.encoder_attn.o_proj.weight"]

residual = dec_x
x_norm = rms_norm(dec_x, c_ln_w)
cq = F.linear(x_norm, c_q_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
ck = F.linear(x_full, c_k_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
cv = F.linear(x_full, c_v_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
scores = torch.matmul(cq, ck.transpose(-2, -1)) * scale
attn = F.softmax(scores, dim=-1)
out = torch.matmul(attn, cv).permute(0, 2, 1, 3).reshape(1, 1, dim)
out = F.linear(out, c_o_w)
dec_x = residual + out
print(f"Dec layer 0 after cross-attn: mean={dec_x.mean().item():.6f}, first 5: {dec_x[0,0,:5].tolist()}")

# SwiGLU FFN
ff_ln_w = weights["model.decoder.layers.0.final_layernorm.weight"]
ff_up_w = weights["model.decoder.layers.0.mlp.fc1.weight"]
ff_up_b = weights["model.decoder.layers.0.mlp.fc1.bias"]
ff_down_w = weights["model.decoder.layers.0.mlp.fc2.weight"]
ff_down_b = weights["model.decoder.layers.0.mlp.fc2.bias"]

residual = dec_x
x_norm = rms_norm(dec_x, ff_ln_w)
fc1 = F.linear(x_norm, ff_up_w, ff_up_b)  # [1, 1, 2304]
inter = fc1.shape[-1] // 2
gate = fc1[..., :inter]
value = fc1[..., inter:]
x_ff = F.silu(gate) * value
x_ff = F.linear(x_ff, ff_down_w, ff_down_b)
dec_x = residual + x_ff
print(f"Dec layer 0 after SwiGLU FFN: mean={dec_x.mean().item():.6f}, first 5: {dec_x[0,0,:5].tolist()}")

# Logits (weight tying)
dec_final_ln = weights["model.decoder.norm.weight"]
dec_x_norm = rms_norm(dec_x, dec_final_ln)
logits = F.linear(dec_x_norm, embed_w)  # [1, 1, 32768]
top5 = torch.topk(logits[0, 0], 5)
print(f"\nLogits top 5: ids={top5.indices.tolist()}, vals={top5.values.tolist()}")

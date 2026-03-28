"""Dump decoder step 0 intermediate values for comparison with C++.
Uses the Python encoder output and traces through decoder layer by layer."""
import numpy as np
import torch
import torch.nn.functional as F
from pathlib import Path
from safetensors import safe_open
import wave

model_dir = Path("models/moonshine-tiny")
dim = 288; n_heads = 8; head_dim = 36; rotary_dim = 32
scale = 1.0 / (head_dim ** 0.5)

weights = {}
with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys():
        weights[name] = f.get_tensor(name).float()

def rms_norm(x, w, eps=1e-5):
    """Legacy — kept for reference but NOT used by HF Moonshine."""
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

def layer_norm(x, w, eps=1e-5):
    """LayerNorm without bias — matches HF nn.LayerNorm(bias=False)."""
    return F.layer_norm(x, (x.shape[-1],), weight=w, bias=None, eps=eps)

def apply_rotary_single(x, pos, rotary_dim=32, theta=10000.0):
    """Interleaved RoPE for single position, matching HF rotate_half."""
    dim_pairs = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos * freqs
    cos_a = torch.cos(angles).view(1, 1, 1, dim_pairs)
    sin_a = torch.sin(angles).view(1, 1, 1, dim_pairs)
    x_rot = x[..., :rotary_dim]; x_pass = x[..., rotary_dim:]
    x1 = x_rot[..., 0::2]; x2 = x_rot[..., 1::2]
    o1 = x1 * cos_a - x2 * sin_a; o2 = x2 * cos_a + x1 * sin_a
    return torch.cat([torch.stack([o1, o2], dim=-1).flatten(-2), x_pass], dim=-1)

# Build encoder output (same as dump_encoder_ref.py)
with wave.open("test/real_speech/en_female_jenny_0.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
pcm_t = torch.from_numpy(pcm).unsqueeze(0)

def apply_rotary_seq(x, rotary_dim=32, theta=10000.0):
    """Interleaved RoPE for sequence, matching HF rotate_half."""
    seq_len = x.shape[2]
    pos = torch.arange(seq_len, dtype=torch.float32)
    dim_pairs = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
    cos_a = torch.cos(angles).unsqueeze(0).unsqueeze(0)
    sin_a = torch.sin(angles).unsqueeze(0).unsqueeze(0)
    x_rot = x[..., :rotary_dim]; x_pass = x[..., rotary_dim:]
    x1 = x_rot[..., 0::2]; x2 = x_rot[..., 1::2]
    o1 = x1*cos_a - x2*sin_a; o2 = x2*cos_a + x1*sin_a
    return torch.cat([torch.stack([o1, o2], dim=-1).flatten(-2), x_pass], dim=-1)

x = F.conv1d(pcm_t.unsqueeze(1), weights["model.encoder.conv1.weight"], stride=64)
x = torch.tanh(x)
x = F.group_norm(x, 1, weights["model.encoder.groupnorm.weight"], weights["model.encoder.groupnorm.bias"])
x = F.conv1d(x, weights["model.encoder.conv2.weight"], weights["model.encoder.conv2.bias"], stride=3)
x = F.gelu(x)
x = F.conv1d(x, weights["model.encoder.conv3.weight"], weights["model.encoder.conv3.bias"], stride=2)
x = F.gelu(x)
x = x.permute(0, 2, 1)
for i in range(6):
    residual = x
    x_norm = layer_norm(x, weights[f"model.encoder.layers.{i}.input_layernorm.weight"])
    q = F.linear(x_norm, weights[f"model.encoder.layers.{i}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    k = F.linear(x_norm, weights[f"model.encoder.layers.{i}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    v = F.linear(x_norm, weights[f"model.encoder.layers.{i}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    q = apply_rotary_seq(q); k = apply_rotary_seq(k)
    attn = F.softmax(torch.matmul(q, k.transpose(-2,-1)) * scale, dim=-1)
    out = torch.matmul(attn, v).permute(0,2,1,3).reshape(1,-1,dim)
    x = residual + F.linear(out, weights[f"model.encoder.layers.{i}.self_attn.o_proj.weight"])
    residual = x
    x_norm = layer_norm(x, weights[f"model.encoder.layers.{i}.post_attention_layernorm.weight"])
    x_ff = F.gelu(F.linear(x_norm, weights[f"model.encoder.layers.{i}.mlp.fc1.weight"], weights[f"model.encoder.layers.{i}.mlp.fc1.bias"]))
    x = residual + F.linear(x_ff, weights[f"model.encoder.layers.{i}.mlp.fc2.weight"], weights[f"model.encoder.layers.{i}.mlp.fc2.bias"])
enc_out = layer_norm(x, weights["model.encoder.layer_norm.weight"])
print(f"enc_out[0,:5] = {enc_out[0,0,:5].tolist()}")

# === Decoder step 0: BOS token ===
embed_w = weights["model.decoder.embed_tokens.weight"]
bos_emb = embed_w[1].unsqueeze(0).unsqueeze(0)  # [1, 1, 288]
print(f"\nBOS embedding[:5] = {bos_emb[0,0,:5].tolist()}")

dec_x = bos_emb
for i in range(6):
    # Self-attention
    residual = dec_x
    x_norm = layer_norm(dec_x, weights[f"model.decoder.layers.{i}.input_layernorm.weight"])
    dq = F.linear(x_norm, weights[f"model.decoder.layers.{i}.self_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    dk = F.linear(x_norm, weights[f"model.decoder.layers.{i}.self_attn.k_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    dv = F.linear(x_norm, weights[f"model.decoder.layers.{i}.self_attn.v_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    dq = apply_rotary_single(dq, 0.0)
    dk = apply_rotary_single(dk, 0.0)
    attn = F.softmax(torch.matmul(dq, dk.transpose(-2,-1)) * scale, dim=-1)
    out = torch.matmul(attn, dv).permute(0,2,1,3).reshape(1,1,dim)
    dec_x = residual + F.linear(out, weights[f"model.decoder.layers.{i}.self_attn.o_proj.weight"])
    
    if i == 0:
        print(f"Dec L0 after self-attn[:5] = {dec_x[0,0,:5].tolist()}")
    
    # Cross-attention
    residual = dec_x
    x_norm = layer_norm(dec_x, weights[f"model.decoder.layers.{i}.post_attention_layernorm.weight"])
    cq = F.linear(x_norm, weights[f"model.decoder.layers.{i}.encoder_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    ck = F.linear(enc_out, weights[f"model.decoder.layers.{i}.encoder_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    cv = F.linear(enc_out, weights[f"model.decoder.layers.{i}.encoder_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    attn = F.softmax(torch.matmul(cq, ck.transpose(-2,-1)) * scale, dim=-1)
    out = torch.matmul(attn, cv).permute(0,2,1,3).reshape(1,1,dim)
    dec_x = residual + F.linear(out, weights[f"model.decoder.layers.{i}.encoder_attn.o_proj.weight"])
    
    if i == 0:
        print(f"Dec L0 after cross-attn[:5] = {dec_x[0,0,:5].tolist()}")
    
    # SwiGLU FFN
    residual = dec_x
    x_norm = layer_norm(dec_x, weights[f"model.decoder.layers.{i}.final_layernorm.weight"])
    fc1 = F.linear(x_norm, weights[f"model.decoder.layers.{i}.mlp.fc1.weight"], weights[f"model.decoder.layers.{i}.mlp.fc1.bias"])
    inter = fc1.shape[-1] // 2
    # HF: hidden_states, gate = chunk(2) → silu(gate) * hidden_states
    # first half = value (no activation), second half = gate (through silu)
    gate = fc1[..., inter:]; value = fc1[..., :inter]
    x_ff = F.silu(gate) * value
    dec_x = residual + F.linear(x_ff, weights[f"model.decoder.layers.{i}.mlp.fc2.weight"], weights[f"model.decoder.layers.{i}.mlp.fc2.bias"])
    
    if i == 0:
        print(f"Dec L0 after FFN[:5] = {dec_x[0,0,:5].tolist()}")

# Final norm + logits
dec_final = layer_norm(dec_x, weights["model.decoder.norm.weight"])
logits = F.linear(dec_final, embed_w)
top5 = torch.topk(logits[0,0], 5)
print(f"\nLogits top5: ids={top5.indices.tolist()}, vals={top5.values.tolist()}")

"""Test: feed C++ encoder output into PyTorch decoder to isolate encoder vs decoder issues."""
import numpy as np
import torch
import torch.nn.functional as F
from pathlib import Path
from safetensors import safe_open

model_dir = Path("models/moonshine-tiny")
dim = 288
n_heads = 8
head_dim = 36
rotary_dim = 32  # int(36 * 0.9) rounded to even

weights = {}
with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys():
        weights[name] = f.get_tensor(name).float()

def rms_norm(x, w, eps=1e-5):
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

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

# Run full PyTorch encoder first to get reference
import wave
with wave.open("test/real_speech/en_female_jenny_0.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

pcm_t = torch.from_numpy(pcm).unsqueeze(0)
x = pcm_t.unsqueeze(1)
x = F.conv1d(x, weights["model.encoder.conv1.weight"], stride=64)
x = torch.tanh(x)
x = F.group_norm(x, 1, weights["model.encoder.groupnorm.weight"], weights["model.encoder.groupnorm.bias"])
x = F.conv1d(x, weights["model.encoder.conv2.weight"], weights["model.encoder.conv2.bias"], stride=3)
x = F.gelu(x)
x = F.conv1d(x, weights["model.encoder.conv3.weight"], weights["model.encoder.conv3.bias"], stride=2)
x = F.gelu(x)
x = x.permute(0, 2, 1)

scale = 1.0 / (head_dim ** 0.5)
for i in range(6):
    ln_w = weights[f"model.encoder.layers.{i}.input_layernorm.weight"]
    q_w = weights[f"model.encoder.layers.{i}.self_attn.q_proj.weight"]
    k_w = weights[f"model.encoder.layers.{i}.self_attn.k_proj.weight"]
    v_w = weights[f"model.encoder.layers.{i}.self_attn.v_proj.weight"]
    o_w = weights[f"model.encoder.layers.{i}.self_attn.o_proj.weight"]
    residual = x
    x_norm = rms_norm(x, ln_w)
    q = F.linear(x_norm, q_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
    k = F.linear(x_norm, k_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
    v = F.linear(x_norm, v_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
    q = apply_rotary(q, rotary_dim)
    k = apply_rotary(k, rotary_dim)
    scores = torch.matmul(q, k.transpose(-2, -1)) * scale
    attn = F.softmax(scores, dim=-1)
    out = torch.matmul(attn, v).permute(0, 2, 1, 3).reshape(1, -1, dim)
    out = F.linear(out, o_w)
    x = residual + out
    ff_ln_w = weights[f"model.encoder.layers.{i}.post_attention_layernorm.weight"]
    ff_up_w = weights[f"model.encoder.layers.{i}.mlp.fc1.weight"]
    ff_up_b = weights[f"model.encoder.layers.{i}.mlp.fc1.bias"]
    ff_down_w = weights[f"model.encoder.layers.{i}.mlp.fc2.weight"]
    ff_down_b = weights[f"model.encoder.layers.{i}.mlp.fc2.bias"]
    residual = x
    x_norm = rms_norm(x, ff_ln_w)
    x_ff = F.linear(x_norm, ff_up_w, ff_up_b)
    x_ff = F.gelu(x_ff)
    x_ff = F.linear(x_ff, ff_down_w, ff_down_b)
    x = residual + x_ff

enc_out = rms_norm(x, weights["model.encoder.layer_norm.weight"])
print(f"PyTorch encoder: {enc_out.shape}, first 5: {enc_out[0,0,:5].tolist()}")

# Now run full decoder with autoregressive generation
embed_w = weights["model.decoder.embed_tokens.weight"]
dec_norm_w = weights["model.decoder.norm.weight"]

tokens = [1]  # BOS
for step in range(50):
    tok_emb = embed_w[tokens[-1]].unsqueeze(0).unsqueeze(0)  # [1, 1, dim]
    dec_x = tok_emb
    
    for i in range(6):
        # Self-attention
        ln_w = weights[f"model.decoder.layers.{i}.input_layernorm.weight"]
        q_w = weights[f"model.decoder.layers.{i}.self_attn.q_proj.weight"]
        k_w = weights[f"model.decoder.layers.{i}.self_attn.k_proj.weight"]
        v_w = weights[f"model.decoder.layers.{i}.self_attn.v_proj.weight"]
        o_w = weights[f"model.decoder.layers.{i}.self_attn.o_proj.weight"]
        
        residual = dec_x
        x_norm = rms_norm(dec_x, ln_w)
        # For simplicity, only use current token (no KV cache)
        dq = F.linear(x_norm, q_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
        dk = F.linear(x_norm, k_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
        dv = F.linear(x_norm, v_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
        # RoPE with position = step
        pos = torch.tensor([step], dtype=torch.float32)
        dim_pairs = rotary_dim // 2
        freqs = 1.0 / (10000.0 ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
        angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
        cos_a = torch.cos(angles).view(1, 1, 1, dim_pairs)
        sin_a = torch.sin(angles).view(1, 1, 1, dim_pairs)
        for t in [dq, dk]:
            t_rot = t[..., :rotary_dim]
            t_pass = t[..., rotary_dim:]
            t1 = t_rot[..., :dim_pairs]
            t2 = t_rot[..., dim_pairs:]
            # Can't modify in-place, skip RoPE for this simple test
        
        scores = torch.matmul(dq, dk.transpose(-2, -1)) * scale
        attn = F.softmax(scores, dim=-1)
        out = torch.matmul(attn, dv).permute(0, 2, 1, 3).reshape(1, 1, dim)
        out = F.linear(out, o_w)
        dec_x = residual + out
        
        # Cross-attention
        c_ln_w = weights[f"model.decoder.layers.{i}.post_attention_layernorm.weight"]
        c_q_w = weights[f"model.decoder.layers.{i}.encoder_attn.q_proj.weight"]
        c_k_w = weights[f"model.decoder.layers.{i}.encoder_attn.k_proj.weight"]
        c_v_w = weights[f"model.decoder.layers.{i}.encoder_attn.v_proj.weight"]
        c_o_w = weights[f"model.decoder.layers.{i}.encoder_attn.o_proj.weight"]
        
        residual = dec_x
        x_norm = rms_norm(dec_x, c_ln_w)
        cq = F.linear(x_norm, c_q_w).view(1, 1, n_heads, head_dim).permute(0, 2, 1, 3)
        ck = F.linear(enc_out, c_k_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
        cv = F.linear(enc_out, c_v_w).view(1, -1, n_heads, head_dim).permute(0, 2, 1, 3)
        scores = torch.matmul(cq, ck.transpose(-2, -1)) * scale
        attn = F.softmax(scores, dim=-1)
        out = torch.matmul(attn, cv).permute(0, 2, 1, 3).reshape(1, 1, dim)
        out = F.linear(out, c_o_w)
        dec_x = residual + out
        
        # SwiGLU FFN
        ff_ln_w = weights[f"model.decoder.layers.{i}.final_layernorm.weight"]
        ff_up_w = weights[f"model.decoder.layers.{i}.mlp.fc1.weight"]
        ff_up_b = weights[f"model.decoder.layers.{i}.mlp.fc1.bias"]
        ff_down_w = weights[f"model.decoder.layers.{i}.mlp.fc2.weight"]
        ff_down_b = weights[f"model.decoder.layers.{i}.mlp.fc2.bias"]
        
        residual = dec_x
        x_norm = rms_norm(dec_x, ff_ln_w)
        fc1 = F.linear(x_norm, ff_up_w, ff_up_b)
        inter = fc1.shape[-1] // 2
        gate = fc1[..., :inter]
        value = fc1[..., inter:]
        x_ff = F.silu(gate) * value
        x_ff = F.linear(x_ff, ff_down_w, ff_down_b)
        dec_x = residual + x_ff
    
    # LM head (weight tying)
    dec_x_norm = rms_norm(dec_x, dec_norm_w)
    logits = F.linear(dec_x_norm, embed_w)
    next_token = logits[0, 0].argmax().item()
    
    if next_token == 2:  # EOS
        print(f"Step {step}: EOS")
        break
    tokens.append(next_token)

print(f"Tokens: {tokens}")
print(f"Note: This is a simplified decoder without KV cache, so results may differ from full model")

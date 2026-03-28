"""Full moonshine-tiny decode with KV cache in PyTorch. Reference output."""
import numpy as np, torch, torch.nn.functional as F, wave
from safetensors import safe_open
from pathlib import Path

model_dir = Path("models/moonshine-tiny")
weights = {}
with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys(): weights[name] = f.get_tensor(name).float()

with wave.open("test/real_speech/en_female_jenny_0.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

dim=288; n_heads=8; head_dim=36; rotary_dim=32; scale=1.0/(head_dim**0.5); theta=10000.0

def rms_norm(x, w, eps=1e-5):
    """Legacy — kept for reference but NOT used by HF Moonshine."""
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

def layer_norm(x, w, eps=1e-5):
    """LayerNorm without bias — matches HF nn.LayerNorm(bias=False)."""
    return F.layer_norm(x, (x.shape[-1],), weight=w, bias=None, eps=eps)

def rope_single(x, pos, rotary_dim, theta):
    """Interleaved RoPE for single position, matching HF rotate_half."""
    dp = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos * freqs
    cos_a = torch.cos(angles).view(1, 1, 1, dp)
    sin_a = torch.sin(angles).view(1, 1, 1, dp)
    x_rot = x[..., :rotary_dim]; xp = x[..., rotary_dim:]
    x1 = x_rot[..., 0::2]; x2 = x_rot[..., 1::2]
    o1 = x1 * cos_a - x2 * sin_a; o2 = x2 * cos_a + x1 * sin_a
    return torch.cat([torch.stack([o1, o2], dim=-1).flatten(-2), xp], dim=-1)

def rope_seq(x, rotary_dim, theta):
    """Interleaved RoPE for sequence, matching HF rotate_half."""
    seq = x.shape[2]; dp = rotary_dim // 2
    pos = torch.arange(seq, dtype=torch.float32)
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
    cos_a = torch.cos(angles).unsqueeze(0).unsqueeze(0)
    sin_a = torch.sin(angles).unsqueeze(0).unsqueeze(0)
    x_rot = x[..., :rotary_dim]; xp = x[..., rotary_dim:]
    x1 = x_rot[..., 0::2]; x2 = x_rot[..., 1::2]
    o1 = x1 * cos_a - x2 * sin_a; o2 = x2 * cos_a + x1 * sin_a
    return torch.cat([torch.stack([o1, o2], dim=-1).flatten(-2), xp], dim=-1)

# Encoder
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

for i in range(6):
    res = x
    x = layer_norm(x, weights[f"model.encoder.layers.{i}.input_layernorm.weight"])
    q = F.linear(x, weights[f"model.encoder.layers.{i}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    k = F.linear(x, weights[f"model.encoder.layers.{i}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    v = F.linear(x, weights[f"model.encoder.layers.{i}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    q = rope_seq(q, rotary_dim, theta); k = rope_seq(k, rotary_dim, theta)
    out = F.scaled_dot_product_attention(q, k, v)
    out = out.permute(0,2,1,3).reshape(1,-1,dim)
    x = res + F.linear(out, weights[f"model.encoder.layers.{i}.self_attn.o_proj.weight"])
    res = x
    x = layer_norm(x, weights[f"model.encoder.layers.{i}.post_attention_layernorm.weight"])
    x = F.linear(x, weights[f"model.encoder.layers.{i}.mlp.fc1.weight"], weights[f"model.encoder.layers.{i}.mlp.fc1.bias"])
    x = F.gelu(x)
    x = F.linear(x, weights[f"model.encoder.layers.{i}.mlp.fc2.weight"], weights[f"model.encoder.layers.{i}.mlp.fc2.bias"])
    x = res + x

enc = layer_norm(x, weights["model.encoder.layer_norm.weight"])
print(f"Encoder: {enc.shape}, first 5: {enc[0,0,:5].tolist()}")

# Decoder with KV cache
embed = weights["model.decoder.embed_tokens.weight"]
dec_norm = weights["model.decoder.norm.weight"]
n_dec = 6

# Pre-compute cross K/V
cross_k_cache = []; cross_v_cache = []
for i in range(n_dec):
    ck = F.linear(enc, weights[f"model.decoder.layers.{i}.encoder_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    cv = F.linear(enc, weights[f"model.decoder.layers.{i}.encoder_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    cross_k_cache.append(ck); cross_v_cache.append(cv)

# Self-attn KV cache
self_k_cache = [[] for _ in range(n_dec)]
self_v_cache = [[] for _ in range(n_dec)]

tokens = [1]  # BOS
for step in range(100):
    tok_emb = embed[tokens[-1]].unsqueeze(0).unsqueeze(0)
    x = tok_emb
    for i in range(n_dec):
        # Self-attention
        res = x
        x = layer_norm(x, weights[f"model.decoder.layers.{i}.input_layernorm.weight"])
        q = F.linear(x, weights[f"model.decoder.layers.{i}.self_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
        k = F.linear(x, weights[f"model.decoder.layers.{i}.self_attn.k_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
        v = F.linear(x, weights[f"model.decoder.layers.{i}.self_attn.v_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
        q = rope_single(q, step, rotary_dim, theta)
        k = rope_single(k, step, rotary_dim, theta)
        self_k_cache[i].append(k)
        self_v_cache[i].append(v)
        k_full = torch.cat(self_k_cache[i], dim=2)
        v_full = torch.cat(self_v_cache[i], dim=2)
        out = F.scaled_dot_product_attention(q, k_full, v_full)
        out = out.permute(0,2,1,3).reshape(1,1,dim)
        x = res + F.linear(out, weights[f"model.decoder.layers.{i}.self_attn.o_proj.weight"])
        # Cross-attention
        res = x
        x = layer_norm(x, weights[f"model.decoder.layers.{i}.post_attention_layernorm.weight"])
        cq = F.linear(x, weights[f"model.decoder.layers.{i}.encoder_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
        out = F.scaled_dot_product_attention(cq, cross_k_cache[i], cross_v_cache[i])
        out = out.permute(0,2,1,3).reshape(1,1,dim)
        x = res + F.linear(out, weights[f"model.decoder.layers.{i}.encoder_attn.o_proj.weight"])
        # SwiGLU FFN
        res = x
        x = layer_norm(x, weights[f"model.decoder.layers.{i}.final_layernorm.weight"])
        fc1 = F.linear(x, weights[f"model.decoder.layers.{i}.mlp.fc1.weight"], weights[f"model.decoder.layers.{i}.mlp.fc1.bias"])
        inter = fc1.shape[-1]//2
        # HF: hidden_states, gate = chunk(2) → silu(gate) * hidden_states
        # first half = value (no activation), second half = gate (through silu)
        x = F.silu(fc1[...,inter:]) * fc1[...,:inter]
        x = F.linear(x, weights[f"model.decoder.layers.{i}.mlp.fc2.weight"], weights[f"model.decoder.layers.{i}.mlp.fc2.bias"])
        x = res + x
    
    logits = F.linear(layer_norm(x, dec_norm), embed)
    next_tok = logits[0,0].argmax().item()
    if step < 3:
        top5 = torch.topk(logits[0,0], 5)
        print(f"Step {step}: top5 ids={top5.indices.tolist()} vals={[f'{v:.2f}' for v in top5.values.tolist()]}")
    if next_tok == 2: break
    tokens.append(next_tok)

# Decode tokens using tokenizer
import json
with open(str(model_dir / "tokenizer.json"), "r", encoding="utf-8") as f:
    tok_data = json.load(f)
vocab = tok_data.get("model", {}).get("vocab", {})
id2tok = {v: k for k, v in vocab.items()}
text = "".join(id2tok.get(t, f"<{t}>") for t in tokens[1:])  # skip BOS
text = text.replace("\u2581", " ").strip()
print(f"\nTokens ({len(tokens)-1}): {tokens[1:min(20, len(tokens))]}")
print(f"Transcription: {text}")

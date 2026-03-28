#!/usr/bin/env python3
"""Compare HF official decoder vs manual decoder (with LayerNorm + correct SwiGLU).
Uses maclaw_16k.wav. Run test_maclaw_real.py first to convert m4a -> wav."""
import numpy as np, torch, torch.nn.functional as F, wave, json, sys, os
from pathlib import Path

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

wav_path = "test/real_speech/maclaw_16k.wav"
if not os.path.exists(wav_path):
    print("Run test_maclaw_real.py first to convert m4a to wav")
    sys.exit(1)

with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"Audio: {len(pcm)/16000:.1f}s, {len(pcm)} samples")

# ── HF Official ──
from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor
model_dir = "models/moonshine-tiny"
hf_model = AutoModelForSpeechSeq2Seq.from_pretrained(model_dir)
hf_proc = AutoProcessor.from_pretrained(model_dir)

inputs = hf_proc(pcm, return_tensors="pt", sampling_rate=16000)
with torch.no_grad():
    # Get encoder output
    hf_enc = hf_model.model.encoder(input_values=inputs.input_values,
                                     attention_mask=inputs.get("attention_mask"))
    enc_hidden = hf_enc.last_hidden_state
    print(f"HF encoder: {enc_hidden.shape}, first5={enc_hidden[0,0,:5].tolist()}")

    # Decoder step 0: BOS token
    bos = torch.tensor([[1]])
    hf_dec = hf_model.model.decoder(input_ids=bos,
                                     encoder_hidden_states=enc_hidden,
                                     encoder_attention_mask=hf_enc.attention_mask)
    hf_logits = hf_model.proj_out(hf_dec.last_hidden_state)
    hf_top5 = torch.topk(hf_logits[0, 0], 5)
    print(f"HF step0 logits top5: ids={hf_top5.indices.tolist()} vals={[f'{v:.2f}' for v in hf_top5.values.tolist()]}")

    # Full generate for reference
    gen = hf_model.generate(**inputs, max_length=200)
    hf_text = hf_proc.decode(gen[0], skip_special_tokens=True)
    print(f"HF transcription: \"{hf_text}\"")

# ── Manual implementation (LayerNorm + correct SwiGLU) ──
from safetensors import safe_open
dim = 288; n_heads = 8; head_dim = 36; rotary_dim = 32; theta = 10000.0

weights = {}
with safe_open(str(Path(model_dir) / "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys(): weights[name] = f.get_tensor(name).float()

def layer_norm(x, w, eps=1e-5):
    return F.layer_norm(x, (x.shape[-1],), weight=w, bias=None, eps=eps)

def rope_seq(x, rotary_dim, theta):
    """Interleaved RoPE matching HF: x1=x[...,0::2], x2=x[...,1::2]"""
    seq = x.shape[2]; dp = rotary_dim // 2
    pos = torch.arange(seq, dtype=torch.float32)
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
    cos_a = torch.cos(angles).unsqueeze(0).unsqueeze(0)
    sin_a = torch.sin(angles).unsqueeze(0).unsqueeze(0)
    x_rot = x[..., :rotary_dim]; xp = x[..., rotary_dim:]
    x1 = x_rot[..., 0::2]; x2 = x_rot[..., 1::2]
    o1 = x1 * cos_a - x2 * sin_a
    o2 = x2 * cos_a + x1 * sin_a
    rot_out = torch.stack([o1, o2], dim=-1).flatten(-2)
    return torch.cat([rot_out, xp], dim=-1)

def rope_single(x, pos, rotary_dim, theta):
    """Interleaved RoPE for single position, matching HF rotate_half"""
    dp = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos * freqs
    cos_a = torch.cos(angles).view(1, 1, 1, dp)
    sin_a = torch.sin(angles).view(1, 1, 1, dp)
    x_rot = x[..., :rotary_dim]; xp = x[..., rotary_dim:]
    x1 = x_rot[..., 0::2]; x2 = x_rot[..., 1::2]
    o1 = x1 * cos_a - x2 * sin_a
    o2 = x2 * cos_a + x1 * sin_a
    rot_out = torch.stack([o1, o2], dim=-1).flatten(-2)
    return torch.cat([rot_out, xp], dim=-1)

# Manual encoder
w = weights
pcm_t = torch.from_numpy(pcm).unsqueeze(0)
x = F.conv1d(pcm_t.unsqueeze(1), w["model.encoder.conv1.weight"], stride=64)
x = torch.tanh(x)
x = F.group_norm(x, 1, w["model.encoder.groupnorm.weight"], w["model.encoder.groupnorm.bias"])
x = F.conv1d(x, w["model.encoder.conv2.weight"], w["model.encoder.conv2.bias"], stride=3); x = F.gelu(x)
x = F.conv1d(x, w["model.encoder.conv3.weight"], w["model.encoder.conv3.bias"], stride=2); x = F.gelu(x)
x = x.permute(0, 2, 1)
for i in range(6):
    res = x; x = layer_norm(x, w[f"model.encoder.layers.{i}.input_layernorm.weight"])
    q = F.linear(x, w[f"model.encoder.layers.{i}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    k = F.linear(x, w[f"model.encoder.layers.{i}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    v = F.linear(x, w[f"model.encoder.layers.{i}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    q = rope_seq(q, rotary_dim, theta); k = rope_seq(k, rotary_dim, theta)
    out = F.scaled_dot_product_attention(q, k, v).permute(0,2,1,3).reshape(1,-1,dim)
    x = res + F.linear(out, w[f"model.encoder.layers.{i}.self_attn.o_proj.weight"])
    res = x; x = layer_norm(x, w[f"model.encoder.layers.{i}.post_attention_layernorm.weight"])
    x = F.gelu(F.linear(x, w[f"model.encoder.layers.{i}.mlp.fc1.weight"], w[f"model.encoder.layers.{i}.mlp.fc1.bias"]))
    x = res + F.linear(x, w[f"model.encoder.layers.{i}.mlp.fc2.weight"], w[f"model.encoder.layers.{i}.mlp.fc2.bias"])
enc = layer_norm(x, w["model.encoder.layer_norm.weight"])
print(f"\nManual encoder: {enc.shape}, first5={enc[0,0,:5].tolist()}")
enc_diff = (enc_hidden - enc).abs().max().item()
print(f"Encoder diff (HF vs manual): max_abs={enc_diff:.6f}")

# Manual decoder step 0
embed = w["model.decoder.embed_tokens.weight"]
dec_norm = w["model.decoder.norm.weight"]
# Pre-compute cross KV
ck_c = []; cv_c = []
for i in range(6):
    p = f"model.decoder.layers.{i}"
    ck_c.append(F.linear(enc, w[f"{p}.encoder_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3))
    cv_c.append(F.linear(enc, w[f"{p}.encoder_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3))

x = embed[1].unsqueeze(0).unsqueeze(0)  # BOS embedding
for i in range(6):
    p = f"model.decoder.layers.{i}"
    # Self-attention
    res = x; x = layer_norm(x, w[f"{p}.input_layernorm.weight"])
    q = F.linear(x, w[f"{p}.self_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    k = F.linear(x, w[f"{p}.self_attn.k_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    v = F.linear(x, w[f"{p}.self_attn.v_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    q = rope_single(q, 0, rotary_dim, theta); k = rope_single(k, 0, rotary_dim, theta)
    out = F.scaled_dot_product_attention(q, k, v).permute(0,2,1,3).reshape(1,1,dim)
    x = res + F.linear(out, w[f"{p}.self_attn.o_proj.weight"])
    # Cross-attention
    res = x; x = layer_norm(x, w[f"{p}.post_attention_layernorm.weight"])
    cq = F.linear(x, w[f"{p}.encoder_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
    out = F.scaled_dot_product_attention(cq, ck_c[i], cv_c[i]).permute(0,2,1,3).reshape(1,1,dim)
    x = res + F.linear(out, w[f"{p}.encoder_attn.o_proj.weight"])
    # SwiGLU FFN — correct order: silu(second_half) * first_half
    res = x; x = layer_norm(x, w[f"{p}.final_layernorm.weight"])
    fc1 = F.linear(x, w[f"{p}.mlp.fc1.weight"], w[f"{p}.mlp.fc1.bias"])
    inter = fc1.shape[-1] // 2
    x = res + F.linear(F.silu(fc1[..., inter:]) * fc1[..., :inter],
                        w[f"{p}.mlp.fc2.weight"], w[f"{p}.mlp.fc2.bias"])

logits = F.linear(layer_norm(x, dec_norm), embed)
manual_top5 = torch.topk(logits[0, 0], 5)
print(f"Manual step0 logits top5: ids={manual_top5.indices.tolist()} vals={[f'{v:.2f}' for v in manual_top5.values.tolist()]}")

# Compare
logit_diff = (hf_logits[0, 0] - logits[0, 0]).abs()
print(f"\nLogits diff: max_abs={logit_diff.max().item():.6f}, mean={logit_diff.mean().item():.6f}")
print(f"HF top1={hf_top5.indices[0].item()}, Manual top1={manual_top5.indices[0].item()}, MATCH={hf_top5.indices[0].item() == manual_top5.indices[0].item()}")

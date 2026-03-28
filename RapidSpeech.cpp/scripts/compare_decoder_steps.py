#!/usr/bin/env python3
"""Decoder step 0 vs step 1 layer-by-layer comparison against HF official model.

For each decoder step, prints per-layer intermediate values:
  - After self-attention (with KV cache for step 1+)
  - After cross-attention
  - After SwiGLU FFN
  - Logits top5

Compares manual implementation (matching C++ logic) against HF transformers.

Usage:
  cd RapidSpeech.cpp
  python scripts/compare_decoder_steps.py
"""
import numpy as np, torch, torch.nn.functional as F, wave, sys, os
from pathlib import Path
from safetensors import safe_open

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

# ── Config ──
model_dir = Path("models/moonshine-tiny")
dim = 288; n_heads = 8; head_dim = 36; rotary_dim = 32; n_dec = 6
theta = 10000.0; scale = 1.0 / (head_dim ** 0.5)
NUM_STEPS = 3  # compare this many decoder steps

# ── Load audio ──
wav_path = "test/real_speech/en_female_jenny_0.wav"
if not os.path.exists(wav_path):
    # fallback
    wav_path = "test/real_speech/maclaw_16k.wav"
if not os.path.exists(wav_path):
    print(f"No test wav found. Place a 16kHz wav at test/real_speech/")
    sys.exit(1)

with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"Audio: {wav_path}, {len(pcm)/16000:.1f}s, {len(pcm)} samples")

# ── Load weights ──
weights = {}
with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys():
        weights[name] = f.get_tensor(name).float()

# ── Helpers ──
def layer_norm(x, w, eps=1e-5):
    return F.layer_norm(x, (x.shape[-1],), weight=w, bias=None, eps=eps)

def rope_seq(x, rotary_dim, theta):
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
    return torch.cat([torch.stack([o1, o2], dim=-1).flatten(-2), xp], dim=-1)

def rope_single(x, pos, rotary_dim, theta):
    dp = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos * freqs
    cos_a = torch.cos(angles).view(1, 1, 1, dp)
    sin_a = torch.sin(angles).view(1, 1, 1, dp)
    x_rot = x[..., :rotary_dim]; xp = x[..., rotary_dim:]
    x1 = x_rot[..., 0::2]; x2 = x_rot[..., 1::2]
    o1 = x1 * cos_a - x2 * sin_a
    o2 = x2 * cos_a + x1 * sin_a
    return torch.cat([torch.stack([o1, o2], dim=-1).flatten(-2), xp], dim=-1)

def fmt5(t):
    """Format first 5 values of a tensor."""
    vals = t.flatten()[:5].tolist()
    return "[" + ", ".join(f"{v:.6f}" for v in vals) + "]"

def compare(name, a, b):
    """Print max/mean abs diff between two tensors."""
    diff = (a - b).abs()
    mx = diff.max().item()
    mn = diff.mean().item()
    status = "OK" if mx < 1e-4 else ("WARN" if mx < 1e-2 else "FAIL")
    print(f"  {status} {name}: max_diff={mx:.8f}, mean_diff={mn:.8f}")
    return mx


# ══════════════════════════════════════════════════════════════
# HF Official Model — extract per-layer intermediates via hooks
# ══════════════════════════════════════════════════════════════
print("\n" + "="*70)
print("Loading HF model...")
from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor

hf_model = AutoModelForSpeechSeq2Seq.from_pretrained(str(model_dir))
hf_proc = AutoProcessor.from_pretrained(str(model_dir))
hf_model.eval()

inputs = hf_proc(pcm, return_tensors="pt", sampling_rate=16000)

# We'll manually drive the HF decoder step by step to capture intermediates.
# HF MoonshineDecoder layers have: input_layernorm, self_attn, post_attention_layernorm,
# encoder_attn, final_layernorm, mlp

with torch.no_grad():
    # Encoder
    hf_enc = hf_model.model.encoder(input_values=inputs.input_values,
                                     attention_mask=inputs.get("attention_mask"))
    enc_hidden = hf_enc.last_hidden_state
    print(f"HF encoder: {enc_hidden.shape}, first5={fmt5(enc_hidden[0,0])}")

    # Step-by-step decoder with past_key_values
    hf_step_data = []  # list of dicts per step
    past_kv = None
    cur_ids = torch.tensor([[1]])  # BOS

    for step in range(NUM_STEPS):
        # Run HF decoder with output_hidden_states to get layer outputs
        # But we need per-sublayer granularity, so we hook into the model
        layer_intermediates = {}

        hooks = []
        for li in range(n_dec):
            layer_obj = hf_model.model.decoder.layers[li]

            # After self_attn (the residual add happens inside the layer)
            # HF MoonshineDecoderLayer forward:
            #   residual = hidden_states
            #   hidden_states = self.input_layernorm(hidden_states)
            #   hidden_states = self.self_attn(...)
            #   hidden_states = residual + hidden_states
            #   <-- we want this value
            #   residual = hidden_states
            #   hidden_states = self.post_attention_layernorm(hidden_states)
            #   hidden_states = self.encoder_attn(...)
            #   hidden_states = residual + hidden_states
            #   <-- and this
            #   residual = hidden_states
            #   hidden_states = self.final_layernorm(hidden_states)
            #   hidden_states = self.mlp(hidden_states)
            #   hidden_states = residual + hidden_states
            #   <-- and this

            # We can't easily hook between residual adds in HF's forward.
            # Instead, hook the norm layers' inputs (which equal the post-residual values).
            def make_hook(layer_idx, point):
                def hook_fn(module, input, output):
                    # input[0] is the hidden_states going INTO the norm
                    # which is the output of the previous residual add
                    layer_intermediates[(layer_idx, point)] = input[0].detach().clone()
                return hook_fn

            # post_attention_layernorm input = after self-attn residual add
            h1 = layer_obj.post_attention_layernorm.register_forward_hook(
                make_hook(li, "after_self_attn"))
            hooks.append(h1)

            # final_layernorm input = after cross-attn residual add
            h2 = layer_obj.final_layernorm.register_forward_hook(
                make_hook(li, "after_cross_attn"))
            hooks.append(h2)

        # Also hook the final decoder norm to get the last layer's FFN output
        def final_norm_hook(module, input, output):
            layer_intermediates[("final", "before_norm")] = input[0].detach().clone()
        h_final = hf_model.model.decoder.norm.register_forward_hook(final_norm_hook)
        hooks.append(h_final)

        # Run decoder
        dec_out = hf_model.model.decoder(
            input_ids=cur_ids,
            encoder_hidden_states=enc_hidden,
            past_key_values=past_kv,
            use_cache=True,
        )
        past_kv = dec_out.past_key_values
        logits = hf_model.proj_out(dec_out.last_hidden_state)

        # Remove hooks
        for h in hooks:
            h.remove()

        # For the last layer's FFN output, we use the final norm input
        # But we also need per-layer FFN output. We can infer it:
        # The input to layer[i+1].input_layernorm = output of layer[i] (after FFN residual)
        # For the last layer, it's the final_norm input.
        # For intermediate layers, we'd need to hook input_layernorm of the NEXT layer.
        # Let's re-run with additional hooks for FFN output.

        # Actually, let's just capture after_ffn by hooking input_layernorm of next layer
        # and the final norm. We already have final_norm. For layers 0..4, the FFN output
        # of layer i = input to input_layernorm of layer i+1.
        # But input_layernorm's input IS the hidden_states entering the layer, which is
        # the output of the previous layer's FFN.
        # We need another pass... or we can just compute it.

        # Simpler: run a second pass with hooks on input_layernorm
        hooks2 = []
        layer_intermediates2 = {}
        for li in range(n_dec):
            layer_obj = hf_model.model.decoder.layers[li]
            def make_hook2(layer_idx):
                def hook_fn(module, input, output):
                    layer_intermediates2[layer_idx] = input[0].detach().clone()
                return hook_fn
            h = layer_obj.input_layernorm.register_forward_hook(make_hook2(li))
            hooks2.append(h)

        # Re-run (reset past_kv to before this step)
        # Actually we can't easily re-run with the same past_kv state.
        # Let's just derive after_ffn from what we have:
        # after_ffn[layer i] for i < n_dec-1: we don't have it directly from hooks.
        # But: after_self_attn[i+1]'s input_layernorm input would be after_ffn[i].
        # We didn't hook that. Let's just accept we have after_self_attn and after_cross_attn.
        for h in hooks2:
            h.remove()

        step_info = {
            "logits": logits[0, -1].detach().clone(),
            "intermediates": layer_intermediates,
        }
        hf_step_data.append(step_info)

        next_tok = logits[0, -1].argmax().item()
        print(f"\nHF step {step}: next_token={next_tok}, logits_top5={torch.topk(logits[0,-1], 5).indices.tolist()}")
        for li in range(n_dec):
            sa = layer_intermediates.get((li, "after_self_attn"))
            ca = layer_intermediates.get((li, "after_cross_attn"))
            if sa is not None:
                print(f"  HF L{li} after_self_attn: {fmt5(sa[0,-1])}")
            if ca is not None:
                print(f"  HF L{li} after_cross_attn: {fmt5(ca[0,-1])}")

        cur_ids = torch.tensor([[next_tok]])


# ══════════════════════════════════════════════════════════════
# Manual decoder — matches C++ logic, with KV cache
# ══════════════════════════════════════════════════════════════
print("\n" + "="*70)
print("Running manual encoder...")

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
print(f"Manual encoder: {enc.shape}, first5={fmt5(enc[0,0])}")
compare("encoder", enc_hidden, enc)

# Pre-compute cross K/V (done once, like C++)
cross_k_cache = []; cross_v_cache = []
for i in range(n_dec):
    p = f"model.decoder.layers.{i}"
    ck = F.linear(enc, w[f"{p}.encoder_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    cv = F.linear(enc, w[f"{p}.encoder_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    cross_k_cache.append(ck)
    cross_v_cache.append(cv)

# Self-attn KV cache (list of tensors per layer, appended each step)
self_k_cache = [[] for _ in range(n_dec)]
self_v_cache = [[] for _ in range(n_dec)]

embed = w["model.decoder.embed_tokens.weight"]
dec_norm = w["model.decoder.norm.weight"]

tokens = [1]  # BOS
print("\n" + "="*70)
print(f"Comparing {NUM_STEPS} decoder steps layer-by-layer...")

for step in range(NUM_STEPS):
    print(f"\n{'─'*60}")
    print(f"STEP {step}: input token = {tokens[-1]}")
    print(f"{'─'*60}")

    tok_emb = embed[tokens[-1]].unsqueeze(0).unsqueeze(0)  # [1, 1, 288]
    x = tok_emb

    for li in range(n_dec):
        p = f"model.decoder.layers.{li}"

        # ── Self-attention ──
        res = x
        x_norm = layer_norm(x, w[f"{p}.input_layernorm.weight"])
        q = F.linear(x_norm, w[f"{p}.self_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
        k_new = F.linear(x_norm, w[f"{p}.self_attn.k_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
        v_new = F.linear(x_norm, w[f"{p}.self_attn.v_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)

        # RoPE at position = step
        q = rope_single(q, step, rotary_dim, theta)
        k_new = rope_single(k_new, step, rotary_dim, theta)

        # Append to KV cache
        self_k_cache[li].append(k_new)
        self_v_cache[li].append(v_new)

        # Debug: dump K/V cache values for step 1 layer 0
        if step == 1 and li == 0:
            # The cached K from step 0 (already has RoPE applied)
            cached_k_step0 = self_k_cache[0][0]  # [1, n_heads, 1, head_dim]
            # ggml layout [head_dim, n_heads, 1]: ne[0]=head_dim contiguous
            # so memory = head0[0..35], head1[0..35], ... = [n_heads, head_dim] row-major
            ck_flat = cached_k_step0[0, :, 0, :].contiguous().flatten()  # [n_heads * head_dim]
            print(f"  [DBG] Python cached_k[0] step0 (first 10, ggml flat): {ck_flat[:10].tolist()}")
            # New K at step 1
            nk_flat = k_new[0, :, 0, :].contiguous().flatten()
            print(f"  [DBG] Python new_k[0] step1 (first 10, ggml flat): {nk_flat[:10].tolist()}")
            # Cached V
            cv_flat = self_v_cache[0][0][0, :, 0, :].contiguous().flatten()
            print(f"  [DBG] Python cached_v[0] step0 (first 10, ggml flat): {cv_flat[:10].tolist()}")

        # Full K/V
        k_full = torch.cat(self_k_cache[li], dim=2)
        v_full = torch.cat(self_v_cache[li], dim=2)

        out = F.scaled_dot_product_attention(q, k_full, v_full)
        out = out.permute(0,2,1,3).reshape(1,1,dim)
        x = res + F.linear(out, w[f"{p}.self_attn.o_proj.weight"])

        # Compare with HF
        hf_sa = hf_step_data[step]["intermediates"].get((li, "after_self_attn"))
        print(f"\n  Layer {li} after SELF-ATTN:")
        print(f"    Manual: {fmt5(x[0,-1])}")
        if hf_sa is not None:
            print(f"    HF:     {fmt5(hf_sa[0,-1])}")
            compare(f"L{li}_self_attn", hf_sa[0,-1], x[0,-1])

        # ── Cross-attention ──
        res = x
        x_norm = layer_norm(x, w[f"{p}.post_attention_layernorm.weight"])
        cq = F.linear(x_norm, w[f"{p}.encoder_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
        out = F.scaled_dot_product_attention(cq, cross_k_cache[li], cross_v_cache[li])
        out = out.permute(0,2,1,3).reshape(1,1,dim)
        x = res + F.linear(out, w[f"{p}.encoder_attn.o_proj.weight"])

        hf_ca = hf_step_data[step]["intermediates"].get((li, "after_cross_attn"))
        print(f"  Layer {li} after CROSS-ATTN:")
        print(f"    Manual: {fmt5(x[0,-1])}")
        if hf_ca is not None:
            print(f"    HF:     {fmt5(hf_ca[0,-1])}")
            compare(f"L{li}_cross_attn", hf_ca[0,-1], x[0,-1])

        # ── SwiGLU FFN ──
        res = x
        x_norm = layer_norm(x, w[f"{p}.final_layernorm.weight"])
        fc1 = F.linear(x_norm, w[f"{p}.mlp.fc1.weight"], w[f"{p}.mlp.fc1.bias"])
        inter = fc1.shape[-1] // 2
        # HF: hidden_states, gate = chunk(2) → silu(gate) * hidden_states
        gate = fc1[..., inter:]
        value = fc1[..., :inter]
        x = res + F.linear(F.silu(gate) * value,
                           w[f"{p}.mlp.fc2.weight"], w[f"{p}.mlp.fc2.bias"])

        print(f"  Layer {li} after FFN:")
        print(f"    Manual: {fmt5(x[0,-1])}")

    # Final norm + logits
    logits = F.linear(layer_norm(x, dec_norm), embed)
    top5 = torch.topk(logits[0, 0], 5)
    next_tok = top5.indices[0].item()

    hf_logits = hf_step_data[step]["logits"]
    hf_top5 = torch.topk(hf_logits, 5)

    print(f"\n  LOGITS:")
    print(f"    Manual top5: ids={top5.indices.tolist()} vals={[f'{v:.2f}' for v in top5.values.tolist()]}")
    print(f"    HF     top5: ids={hf_top5.indices.tolist()} vals={[f'{v:.2f}' for v in hf_top5.values.tolist()]}")
    logit_diff = compare("logits", hf_logits, logits[0, 0])

    match = top5.indices[0].item() == hf_top5.indices[0].item()
    print(f"    Top1 match: {'YES' if match else 'NO'} (manual={top5.indices[0].item()}, hf={hf_top5.indices[0].item()})")

    tokens.append(next_tok)

# ══════════════════════════════════════════════════════════════
# Summary
# ══════════════════════════════════════════════════════════════
print(f"\n{'='*70}")
print("SUMMARY")
print(f"{'='*70}")
print(f"Tokens generated: {tokens}")

# Decode tokens
import json
with open(str(model_dir / "tokenizer.json"), "r", encoding="utf-8") as f:
    tok_data = json.load(f)
vocab = tok_data.get("model", {}).get("vocab", {})
id2tok = {v: k for k, v in vocab.items()}
text = "".join(id2tok.get(t, f"<{t}>") for t in tokens[1:])
text = text.replace("\u2581", " ").strip()
print(f"Manual transcription (first {NUM_STEPS} tokens): {text}")

print("\nIf step 0 matches but step 1+ diverges, check:")
print("  1. KV cache write position (step offset)")
print("  2. RoPE position for cached K (should be the step when K was computed)")
print("  3. KV cache memory layout ([head_dim, n_heads, seq] in C++)")
print("  4. Whether cached K already has RoPE applied (it should)")
print("  5. Cross K/V should NOT change between steps")

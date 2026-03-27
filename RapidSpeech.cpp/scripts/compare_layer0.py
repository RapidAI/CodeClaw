"""Compare encoder layer 0 self-attention output between PyTorch and ggml-style computation."""
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

pcm_t = torch.from_numpy(pcm).unsqueeze(0)
x = pcm_t.unsqueeze(1)
x = F.conv1d(x, weights["model.encoder.conv1.weight"], stride=64)
x = torch.tanh(x)
x = F.group_norm(x, 1, weights["model.encoder.groupnorm.weight"], weights["model.encoder.groupnorm.bias"])
x = F.conv1d(x, weights["model.encoder.conv2.weight"], weights["model.encoder.conv2.bias"], stride=3)
x = F.gelu(x)
x = F.conv1d(x, weights["model.encoder.conv3.weight"], weights["model.encoder.conv3.bias"], stride=2)
x = F.gelu(x)
x = x.permute(0, 2, 1)  # [1, 182, 288]

dim = 288; n_heads = 8; head_dim = 36; rotary_dim = 32
scale = 1.0 / (head_dim ** 0.5)

def rms_norm(x, w, eps=1e-5):
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

def apply_rotary(x, rotary_dim, theta=10000.0):
    """NeoX-style RoPE: split first half / second half."""
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

# Standard PyTorch attention
ln_w = weights["model.encoder.layers.0.input_layernorm.weight"]
q_w = weights["model.encoder.layers.0.self_attn.q_proj.weight"]
k_w = weights["model.encoder.layers.0.self_attn.k_proj.weight"]
v_w = weights["model.encoder.layers.0.self_attn.v_proj.weight"]
o_w = weights["model.encoder.layers.0.self_attn.o_proj.weight"]

residual = x
x_norm = rms_norm(x, ln_w)
seq = x.shape[1]

q = F.linear(x_norm, q_w).view(1, seq, n_heads, head_dim).permute(0, 2, 1, 3)
k = F.linear(x_norm, k_w).view(1, seq, n_heads, head_dim).permute(0, 2, 1, 3)
v = F.linear(x_norm, v_w).view(1, seq, n_heads, head_dim).permute(0, 2, 1, 3)
q = apply_rotary(q, rotary_dim)
k = apply_rotary(k, rotary_dim)

# Standard attention
scores_std = torch.matmul(q, k.transpose(-2, -1)) * scale
attn_std = F.softmax(scores_std, dim=-1)
out_std = torch.matmul(attn_std, v).permute(0, 2, 1, 3).reshape(1, seq, dim)
out_std = F.linear(out_std, o_w)
x_std = residual + out_std

# Now simulate ggml-style attention (sdpa_attention)
# Input: q_ggml[head_dim, n_heads, seq], k_ggml[head_dim, n_heads, seq], v_ggml[head_dim, n_heads, seq]
# In ggml, x is [dim, seq]. After mul_mat + reshape3d: [head_dim, n_heads, seq]
# This is equivalent to PyTorch [batch, n_heads, seq, head_dim] but with head_dim as dim0

# Convert PyTorch [1, n_heads, seq, head_dim] to ggml [head_dim, n_heads, seq]
q_ggml = q[0].permute(2, 0, 1)  # [head_dim, n_heads, seq]
k_ggml = k[0].permute(2, 0, 1)
v_ggml = v[0].permute(2, 0, 1)

# sdpa_attention steps:
# 1. scale q
q_ggml_s = q_ggml * scale

# 2. permute to [head_dim, seq, n_heads]
qp = q_ggml_s.permute(0, 2, 1)  # [head_dim, seq, n_heads]
kp = k_ggml.permute(0, 2, 1)
vp = v_ggml.permute(0, 2, 1)

# 3. scores = kp^T @ qp (ggml_mul_mat transposes first arg along dim0)
# kp is [head_dim, seq_k, n_heads], kp^T along dim0 = [seq_k, head_dim, n_heads]
# kp^T @ qp = [seq_k, head_dim] @ [head_dim, seq_q] = [seq_k, seq_q] per head
scores_ggml = torch.bmm(kp.permute(2, 1, 0).reshape(n_heads, seq, head_dim),
                          qp.permute(2, 0, 1).reshape(n_heads, head_dim, seq))
# Wait, this is getting confusing. Let me just simulate ggml_mul_mat directly.
# ggml_mul_mat(a[ne0, ne1, ne2], b[ne0, ne1, ne2]) = a^T @ b per ne2
# a^T transposes dim0 and dim1: a[ne0, ne1] -> [ne1, ne0]
# result: [ne1_a, ne1_b, ne2]

# kp[head_dim, seq_k, n_heads], qp[head_dim, seq_q, n_heads]
# ggml_mul_mat(kp, qp): kp^T[seq_k, head_dim] @ qp[head_dim, seq_q] = [seq_k, seq_q] per n_heads
scores_ggml = torch.zeros(seq, seq, n_heads)
for h in range(n_heads):
    kp_h = kp[:, :, h]  # [head_dim, seq_k]
    qp_h = qp[:, :, h]  # [head_dim, seq_q]
    scores_ggml[:, :, h] = kp_h.T @ qp_h  # [seq_k, seq_q]

# 4. softmax over dim0 (seq_k)
scores_ggml_sm = F.softmax(scores_ggml, dim=0)

# 5. vp_t = permute(vp, 1,0,2,3) -> [seq_k, head_dim, n_heads]
vp_t = vp.permute(1, 0, 2)  # [seq_k, head_dim, n_heads]

# 6. attn_out = ggml_mul_mat(vp_t, scores) = vp_t^T @ scores
# vp_t[seq_k, head_dim, n_heads], scores[seq_k, seq_q, n_heads]
# vp_t^T[head_dim, seq_k] @ scores[seq_k, seq_q] = [head_dim, seq_q] per n_heads
attn_ggml = torch.zeros(head_dim, seq, n_heads)
for h in range(n_heads):
    vp_t_h = vp_t[:, :, h]  # [seq_k, head_dim]
    scores_h = scores_ggml_sm[:, :, h]  # [seq_k, seq_q]
    attn_ggml[:, :, h] = vp_t_h.T @ scores_h  # [head_dim, seq_q]

# 7. permute to [head_dim, n_heads, seq_q], reshape to [dim, seq_q]
attn_ggml_p = attn_ggml.permute(0, 2, 1)  # [head_dim, n_heads, seq]
attn_ggml_flat = attn_ggml_p.reshape(dim, seq)  # [dim, seq]

# Compare with standard: out_std is [1, seq, dim], transpose to [dim, seq]
out_std_t = out_std[0].T  # [dim, seq]

# The ggml attn output (before o_proj) should match standard (before o_proj)
# Standard before o_proj: matmul(attn, v).permute(0,2,1,3).reshape(1,seq,dim) -> [1, seq, dim]
out_std_before_proj = torch.matmul(attn_std, v).permute(0, 2, 1, 3).reshape(1, seq, dim)
out_std_bp_t = out_std_before_proj[0].T  # [dim, seq]

print(f"Standard attn (before proj) [0,:5] = {out_std_bp_t[:5, 0].tolist()}")
print(f"GGML-style attn [0,:5] = {attn_ggml_flat[:5, 0].tolist()}")
print(f"Max diff = {(out_std_bp_t - attn_ggml_flat).abs().max().item():.8f}")
print(f"Mean diff = {(out_std_bp_t - attn_ggml_flat).abs().mean().item():.8f}")

"""Verify sdpa_attention math by simulating ggml operations exactly."""
import torch
import torch.nn.functional as F

torch.manual_seed(42)
head_dim, n_heads, seq = 4, 2, 3  # tiny example

# Create Q, K, V in PyTorch standard layout: [1, n_heads, seq, head_dim]
Q = torch.randn(1, n_heads, seq, head_dim)
K = torch.randn(1, n_heads, seq, head_dim)
V = torch.randn(1, n_heads, seq, head_dim)

# Standard PyTorch attention
scale = 1.0 / (head_dim ** 0.5)
scores_std = torch.matmul(Q, K.transpose(-2, -1)) * scale
attn_std = F.softmax(scores_std, dim=-1)
out_std = torch.matmul(attn_std, V)  # [1, n_heads, seq, head_dim]
# Reshape to [1, seq, dim]
out_std_flat = out_std.permute(0, 2, 1, 3).reshape(1, seq, n_heads * head_dim)
print(f"Standard output [0,0,:] = {out_std_flat[0, 0, :].tolist()}")

# Now simulate ggml layout.
# In ggml, after mul_mat(q_weight, x) + reshape_3d(result, head_dim, n_heads, seq):
# The data in memory is: for each seq position s, for each head h, head_dim values.
# This is the SAME as PyTorch's F.linear output [1, seq, dim] viewed as [1, seq, n_heads, head_dim].
# NOT the same as PyTorch's permuted [1, n_heads, seq, head_dim].

# So ggml's [head_dim, n_heads, seq] in ggml-order (ne[0]=head_dim fastest) corresponds to
# PyTorch's [seq, n_heads, head_dim] in C-order (last dim fastest).
# This is Q BEFORE the permute(0, 2, 1, 3) in PyTorch.

# Let's create the ggml-equivalent tensors from the PyTorch standard layout:
# PyTorch Q is [1, n_heads, seq, head_dim]. To get ggml layout [seq, n_heads, head_dim]:
Q_ggml = Q[0].permute(1, 0, 2)  # [seq, n_heads, head_dim]
K_ggml = K[0].permute(1, 0, 2)
V_ggml = V[0].permute(1, 0, 2)

# In ggml, this is stored as [head_dim, n_heads, seq] (reversed dim order).
# For simulation, we work with the C-order [seq, n_heads, head_dim] directly.

# sdpa_attention steps:
# 1. Scale Q
Q_ggml_s = Q_ggml * scale

# 2. Permute [head_dim, n_heads, seq] -> [head_dim, seq, n_heads]
# In C-order: [seq, n_heads, head_dim] -> [n_heads, seq, head_dim]
# ggml permute(0, 2, 1, 3) swaps ne[1] and ne[2], which in C-order swaps dim[-2] and dim[-3]
# For 3D: swaps dim[0] and dim[1] in C-order
Qp = Q_ggml_s.permute(1, 0, 2).contiguous()  # [n_heads, seq, head_dim]
Kp = K_ggml.permute(1, 0, 2).contiguous()
Vp = V_ggml.permute(1, 0, 2).contiguous()

# 3. ggml_mul_mat(Kp, Qp): Kp^T @ Qp per batch(ne[2] in ggml = dim[0] in C-order = n_heads)
# In C-order: Kp[n_heads, seq_k, head_dim], Qp[n_heads, seq_q, head_dim]
# ggml transposes first arg's ne[0] and ne[1], which in C-order is last two dims.
# So Kp^T in C-order: [n_heads, head_dim, seq_k]
# Kp^T @ Qp: [n_heads, head_dim, seq_k] @ [n_heads, seq_q, head_dim]... that doesn't work.
#
# Wait. ggml_mul_mat(a, b) with a[ne0, ne1, ne2] b[ne0, ne1, ne2]:
# result[ne1_a, ne1_b, ne2_b]
# The operation is: for each batch in ne2: a_slice^T @ b_slice
# where a_slice is [ne0, ne1] and a_slice^T is [ne1, ne0]
# result_slice = [ne1_a, ne0] @ [ne0, ne1_b] = [ne1_a, ne1_b]
#
# In ggml order: Kp[head_dim, seq_k, n_heads], Qp[head_dim, seq_q, n_heads]
# ne0=head_dim, ne1_a=seq_k, ne1_b=seq_q, ne2=n_heads
# result: [seq_k, seq_q, n_heads]
# Kp^T[seq_k, head_dim] @ Qp[head_dim, seq_q] = [seq_k, seq_q] per head
#
# In C-order (reversed): Kp is [n_heads, seq_k, head_dim], Qp is [n_heads, seq_q, head_dim]
# The ggml mul_mat does: for each head h:
#   Kp[h]^T_ggml @ Qp[h]_ggml
# where Kp[h]_ggml is [head_dim, seq_k] = C-order [seq_k, head_dim]
# Kp[h]^T_ggml = [seq_k, head_dim] in ggml = C-order [head_dim, seq_k]
# Kp[h]^T_ggml @ Qp[h]_ggml = [seq_k, head_dim] @ [head_dim, seq_q] in ggml
#   = C-order: [head_dim, seq_k] @ [seq_q, head_dim]... this is confusing.
#
# Let me just use the ggml semantics directly:
# ggml_mul_mat(a, b) = a^T @ b where ^T transposes the first two dims (ne[0], ne[1])
# For 3D, batched over ne[2].
# a = Kp_ggml[head_dim, seq_k, n_heads]
# b = Qp_ggml[head_dim, seq_q, n_heads]
# a^T = [seq_k, head_dim, n_heads]
# result = a^T @ b = [seq_k, head_dim] @ [head_dim, seq_q] = [seq_k, seq_q] per head
#
# In C-order, this means: for each head h:
#   result[h, :, :] = Kp_c[h, :, :].T @ Qp_c[h, :, :]
# where Kp_c[h] is [seq_k, head_dim] and Kp_c[h].T is [head_dim, seq_k]
# result[h] = [head_dim, seq_k] @ [seq_q, head_dim]... NO.
#
# I keep getting confused. Let me just compute it numerically.

scores_ggml = torch.zeros(n_heads, seq, seq)  # C-order [n_heads, seq_k, seq_q] = ggml [seq_q, seq_k, n_heads]
for h in range(n_heads):
    # Kp_c[h] is [seq_k, head_dim], Qp_c[h] is [seq_q, head_dim]
    # ggml: Kp_ggml[h] is [head_dim, seq_k], Kp_ggml[h]^T is [seq_k, head_dim]
    # ggml result = Kp^T @ Qp = [seq_k, head_dim] @ [head_dim, seq_q] = [seq_k, seq_q]
    # In C-order: [seq_q, seq_k] (reversed)
    # So scores_c[h] = Kp_c[h] @ Qp_c[h].T = [seq_k, head_dim] @ [head_dim, seq_q] = [seq_k, seq_q]
    scores_ggml[h] = Kp[h] @ Qp[h].T  # [seq_k, seq_q]

# 4. Softmax over ne[0] in ggml = seq_k
# In C-order, ggml ne[0] corresponds to the LAST dimension.
# scores_ggml C-order is [n_heads, seq_q, seq_k] (ggml [seq_k, seq_q, n_heads])
# Wait - I computed scores_ggml[h] = Kp[h] @ Qp[h].T = [seq_k, seq_q]
# So scores_ggml is [n_heads, seq_k, seq_q] in my C-order tensor.
# ggml ne[0] = seq_k = dim[1] in my C-order (middle dim for 3D).
# ggml_soft_max normalizes over ne[0] = seq_k.
# In my C-order [n_heads, seq_k, seq_q], that's dim=1.
scores_ggml_sm = F.softmax(scores_ggml, dim=1)  # softmax over seq_k (dim 1 in C-order)

# Compare with standard
print(f"\nStandard scores[0,0,0,:] = {attn_std[0,0,0,:].tolist()}")
print(f"GGML scores[0,0,:] = {scores_ggml_sm[0,0,:].tolist()}")
print(f"Match: {torch.allclose(attn_std[0], scores_ggml_sm, atol=1e-6)}")

# 5. vp_t = permute(vp, 1, 0, 2, 3) in ggml = swap ne[0] and ne[1]
# Vp_ggml[head_dim, seq_k, n_heads] -> Vp_t_ggml[seq_k, head_dim, n_heads]
# In C-order: Vp_c[n_heads, seq_k, head_dim] -> Vp_t_c[n_heads, head_dim, seq_k]
Vp_t = Vp.permute(0, 2, 1).contiguous()  # [n_heads, head_dim, seq_k]

# 6. ggml_mul_mat(vp_t, scores)
# vp_t_ggml[seq_k, head_dim, n_heads], scores_ggml[seq_k, seq_q, n_heads]
# vp_t^T = [head_dim, seq_k], scores = [seq_k, seq_q]
# result = vp_t^T @ scores = [head_dim, seq_q] per head
# In C-order: result[h] = Vp_t_c[h].T @ scores_c[h]
# Vp_t_c[h] is [head_dim, seq_k], Vp_t_c[h].T is [seq_k, head_dim]
# result[h] = [seq_k, head_dim] @ [seq_q, seq_k]... NO
# Vp_t_c[h] is [head_dim, seq_k], so Vp_t_c[h].T is [seq_k, head_dim]
# scores_c[h] is [seq_q, seq_k]
# We need: Vp_t_c[h] @ scores_c[h].T = [head_dim, seq_k] @ [seq_k, seq_q] = [head_dim, seq_q]
# But ggml does vp_t^T @ scores, not vp_t @ scores^T.
# In C-order: vp_t_c[h].T @ scores_c[h] = [seq_k, head_dim] @ [seq_q, seq_k]... dims don't match.
#
# I think the issue is that C-order reversal makes the transpose confusing.
# Let me just compute numerically what ggml would produce:

attn_ggml = torch.zeros(n_heads, seq, head_dim)  # C-order [n_heads, seq_q, head_dim] = ggml [head_dim, seq_q, n_heads]
for h in range(n_heads):
    # ggml: vp_t[seq_k, head_dim], vp_t^T = [head_dim, seq_k]
    # ggml: scores[seq_k, seq_q]
    # ggml result = vp_t^T @ scores = [head_dim, seq_k] @ [seq_k, seq_q] = [head_dim, seq_q]
    # In C-order: [seq_q, head_dim]
    # vp_t^T in ggml = Vp_t_c[h].T in numpy sense? No.
    # ggml vp_t has ne[0]=seq_k, ne[1]=head_dim. Transpose swaps ne[0],ne[1] -> [head_dim, seq_k].
    # In C-order: vp_t_c[h] is [head_dim, seq_k]. ggml transpose gives [seq_k, head_dim] in C-order.
    # Wait, ggml ne[0]=seq_k means C-order last dim is seq_k. So C-order is [..., head_dim, seq_k].
    # ggml transpose of [seq_k, head_dim] gives [head_dim, seq_k] in ggml = C-order [seq_k, head_dim].
    #
    # OK I'm going in circles. Let me just directly compute:
    # ggml_mul_mat(a, b) result[i, j, batch] = sum_k a[k, i, batch] * b[k, j, batch]
    # a = vp_t_ggml, b = scores_ggml
    # vp_t_ggml[k, i, batch] where k in [0, seq_k), i in [0, head_dim), batch in [0, n_heads)
    # scores_ggml[k, j, batch] where k in [0, seq_k), j in [0, seq_q), batch in [0, n_heads)
    # result[i, j, batch] = sum_k vp_t_ggml[k, i, batch] * scores_ggml[k, j, batch]
    #
    # vp_t_ggml[k, i, h] = Vp_t in ggml layout. Vp_t = permute(Vp, 1,0,2,3).
    # Vp_ggml[d, s, h] where d=head_dim, s=seq_k, h=n_heads
    # After permute(1,0,2,3): Vp_t_ggml[s, d, h] = Vp_ggml[d, s, h]
    # So vp_t_ggml[k, i, h] = Vp_ggml[i, k, h]
    #
    # scores_ggml[k, j, h] = result of mul_mat(Kp, Qp)
    # = sum_d Kp_ggml[d, k, h] * Qp_ggml[d, j, h]
    #
    # Now Vp_ggml[d, s, h] corresponds to V for head h, position s, dimension d.
    # In PyTorch: V[0, h, s, d]
    #
    # result[i, j, h] = sum_k Vp_ggml[i, k, h] * scores_ggml[k, j, h]
    #                 = sum_k V[0,h,k,i] * scores[k,j,h]
    #
    # Standard: out[0,h,j,i] = sum_k attn[0,h,j,k] * V[0,h,k,i]
    #
    # So result[i, j, h] = sum_k V[0,h,k,i] * scores[k,j,h]
    # Standard: out[0,h,j,i] = sum_k attn[0,h,j,k] * V[0,h,k,i]
    #
    # These are the same IF scores[k,j,h] == attn[0,h,j,k]
    # i.e., scores_ggml is the TRANSPOSE of attn_std per head.
    
    # Let's check:
    pass

# Direct numerical check
print(f"\nattn_std[0,0,:,:] (head 0):")
print(attn_std[0, 0])
print(f"\nscores_ggml_sm[0,:,:] (head 0):")
print(scores_ggml_sm[0])
print(f"\nAre they transposes? {torch.allclose(attn_std[0,0], scores_ggml_sm[0].T, atol=1e-6)}")

# If scores_ggml[k,j,h] == attn[0,h,j,k], then:
# result[i,j,h] = sum_k V[0,h,k,i] * attn[0,h,j,k] = out_std[0,h,j,i]
# So result_ggml[i,j,h] = out_std[0,h,j,i]
# After permute back to [head_dim, n_heads, seq_q] in ggml = C-order [seq_q, n_heads, head_dim]:
# result_final[s, h, d] = result_ggml[d, s, h] = out_std[0, h, s, d]
# After reshape to [dim, seq_q] in ggml = C-order [seq_q, dim]:
# The dim ordering is [h0_d0, h0_d1, ..., h0_d3, h1_d0, ..., h1_d3] per seq position.
# Standard: out_std.permute(0,2,1,3).reshape(1,seq,dim) gives the same ordering.
# So they SHOULD match.

# Let me just compute it:
for h in range(n_heads):
    for j in range(seq):
        for i in range(head_dim):
            val = 0.0
            for k in range(seq):
                val += V[0, h, k, i].item() * scores_ggml_sm[h, k, j].item()
            attn_ggml[h, j, i] = val

# Permute to [seq_q, n_heads, head_dim] then reshape to [seq_q, dim]
attn_ggml_out = attn_ggml.permute(1, 0, 2).reshape(seq, n_heads * head_dim)

print(f"\nStandard output [0,:] = {out_std_flat[0, 0, :].tolist()}")
print(f"GGML output [0,:] = {attn_ggml_out[0, :].tolist()}")
print(f"Match: {torch.allclose(out_std_flat[0], attn_ggml_out, atol=1e-5)}")
print(f"Max diff: {(out_std_flat[0] - attn_ggml_out).abs().max().item():.8f}")

"""Minimal test: manually compute conv1d the same way ggml does (im2col + matmul)
to find where the numerical divergence comes from."""
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

with safe_open(str(model_dir / "model.safetensors"), framework="numpy") as f:
    conv1_w = f.get_tensor("model.encoder.conv1.weight").astype(np.float32)

# PyTorch reference
pcm_t = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(0)  # [1, 1, 70656]
w_t = torch.from_numpy(conv1_w)  # [288, 1, 127]
ref = F.conv1d(pcm_t, w_t, stride=64)  # [1, 288, 1103]
print(f"PyTorch conv1d output: shape={ref.shape}")
print(f"  min={ref.min().item():.6f} max={ref.max().item():.6f}")
print(f"  [0, 0, 551] = {ref[0, 0, 551].item():.6f}")
print(f"  [0, :5, 551] = {ref[0, :5, 551].tolist()}")

# Manual im2col + matmul (simulating ggml)
# ggml stores in column-major. Let's think in ggml terms:
# kernel: ne[0]=127, ne[1]=1, ne[2]=288 (K, IC, OC)
# input: ne[0]=70656, ne[1]=1 (L, IC)
# im2col output: ne[0]=127, ne[1]=1103, ne[2]=1 (IC*K, OL, N)
# 
# im2col extracts patches: for each output position i, extract input[i*stride : i*stride+K]
# This gives a [K, OL] matrix (for IC=1)

K = 127
stride = 64
L = len(pcm)
OL = (L - K) // stride + 1
print(f"\nManual im2col: K={K}, stride={stride}, L={L}, OL={OL}")

# Build im2col matrix: [K, OL]
im2col = np.zeros((K, OL), dtype=np.float32)
for i in range(OL):
    start = i * stride
    im2col[:, i] = pcm[start:start+K]

print(f"im2col shape: {im2col.shape}")
print(f"  im2col[:5, 551] = {im2col[:5, 551]}")

# kernel reshaped: [K*IC, OC] = [127, 288]
# In ggml: kernel ne=[127,1,288], reshape_2d -> [127*1, 288] = [127, 288]
kernel_2d = conv1_w.reshape(288, 127).T  # [127, 288] — but wait, need to check order

# ggml_mul_mat(A, B) computes B^T @ A when A.ne[0]==B.ne[0]
# A = im2col reshaped to [127, 1103]
# B = kernel reshaped to [127, 288]
# Result = B^T @ A = [288, 127] @ [127, 1103] = [288, 1103]
# Then reshape to [OL, OC, N] = [1103, 288, 1]

# Wait, ggml_mul_mat(A, B) where A has shape [ne0, ne1] and B has shape [ne0, ne1_b]
# computes result[i,j] = sum_k A[k,i] * B[k,j]
# So result shape is [ne1_a, ne1_b] = [1103, 288]
# This is: for each output position i and each output channel j:
#   result[i,j] = sum_k im2col[k,i] * kernel[k,j]
# Which is: sum_k input_patch[k] * kernel_weight[k,j]
# This is correct conv1d!

# But the kernel reshape matters. In ggml:
# kernel tensor ne=[127, 1, 288]
# reshape_2d(kernel, 127*1, 288) -> ne[0]=127, ne[1]=288
# The data layout in memory (column-major): kernel[k, oc] = data[k + 127*oc]
# 
# In safetensors (row-major numpy): conv1_w[oc, ic, k] = data[oc*1*127 + ic*127 + k]
# For ic=0: conv1_w[oc, 0, k] = data[oc*127 + k]
# 
# In ggml (column-major): tensor ne=[127, 1, 288]
# element at [k, ic, oc] = data[k + 127*ic + 127*1*oc] = data[k + 127*oc]
# After reshape_2d to [127, 288]: element at [k, oc] = data[k + 127*oc]
# 
# So kernel_2d[k, oc] = conv1_w[oc, 0, k] — this is correct!

kernel_2d_correct = np.zeros((127, 288), dtype=np.float32)
for oc in range(288):
    kernel_2d_correct[:, oc] = conv1_w[oc, 0, :]

# Manual matmul: result[i, j] = sum_k im2col[k, i] * kernel_2d[k, j]
result = im2col.T @ kernel_2d_correct  # [1103, 288]
print(f"\nManual result shape: {result.shape}")
print(f"  min={result.min():.6f} max={result.max():.6f}")
print(f"  result[551, :5] = {result[551, :5]}")

# Compare with PyTorch
# PyTorch output is [1, 288, 1103], so ref[0, oc, t] = result[t, oc]
print(f"\nPyTorch ref[0, :5, 551] = {ref[0, :5, 551].tolist()}")
print(f"Manual result[551, :5] = {result[551, :5].tolist()}")
diff = np.abs(result[551, :5] - ref[0, :5, 551].numpy())
print(f"Diff: {diff}")

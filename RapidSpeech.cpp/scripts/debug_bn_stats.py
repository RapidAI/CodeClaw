#!/usr/bin/env python3
"""Debug BatchNorm statistics to find why all embeddings collapse."""
import os, sys, torch, numpy as np

sys.path.insert(0, os.path.dirname(__file__))
from debug_ecapa_pytorch import ECAPA_TDNN, load_speechbrain_ckpt

ckpt_path = os.path.join(os.path.dirname(__file__), "..", "..", "ecapa-raw", "embedding_model.ckpt")

# Load raw checkpoint
sd = torch.load(ckpt_path, map_location="cpu")

# Check BN stats
print("=== BatchNorm Running Stats ===")
for key in sorted(sd.keys()):
    if 'running_mean' in key or 'running_var' in key:
        v = sd[key]
        print(f"  {key:60s} shape={list(v.shape)} mean={v.mean():.6f} std={v.std():.6f} min={v.min():.6f} max={v.max():.6f}")

print("\n=== BN Weight/Bias ===")
for key in sorted(sd.keys()):
    if ('norm.norm.weight' in key or 'norm.norm.bias' in key) and 'blocks.0' in key:
        v = sd[key]
        print(f"  {key:60s} shape={list(v.shape)} mean={v.mean():.6f} std={v.std():.6f}")

# Check if running_var has very large values (would make BN output near-zero)
print("\n=== Running Var Analysis ===")
for key in sorted(sd.keys()):
    if 'running_var' in key:
        v = sd[key]
        large = (v > 100).sum().item()
        small = (v < 0.01).sum().item()
        print(f"  {key:60s} >100: {large}/{v.numel()}, <0.01: {small}/{v.numel()}")

# Now test: what happens if we manually run BN on different inputs?
print("\n=== Manual BN Test ===")
model = ECAPA_TDNN(n_mels=80, channels=1024, emb_dim=192, res2_scale=8)
model = load_speechbrain_ckpt(model, ckpt_path)
model.eval()

# Check layer0 BN
bn = model.blocks[0].norm
print(f"Layer0 BN: weight mean={bn.weight.data.mean():.6f}, bias mean={bn.bias.data.mean():.6f}")
print(f"  running_mean: mean={bn.running_mean.mean():.6f}, std={bn.running_mean.std():.6f}")
print(f"  running_var:  mean={bn.running_var.mean():.6f}, std={bn.running_var.std():.6f}")

# Test BN with two very different inputs
x1 = torch.randn(1, 1024, 100) * 10  # large values
x2 = torch.randn(1, 1024, 100) * 0.01  # tiny values
with torch.no_grad():
    y1 = bn(x1)
    y2 = bn(x2)
print(f"\n  Input1 (large):  mean={x1.mean():.4f}, std={x1.std():.4f}")
print(f"  Output1:         mean={y1.mean():.4f}, std={y1.std():.4f}")
print(f"  Input2 (small):  mean={x2.mean():.4f}, std={x2.std():.4f}")
print(f"  Output2:         mean={y2.mean():.4f}, std={y2.std():.4f}")
cos = torch.nn.functional.cosine_similarity(y1.flatten().unsqueeze(0), y2.flatten().unsqueeze(0))
print(f"  Cosine(y1, y2):  {cos.item():.6f}")

# Test the full layer0 (conv + bn + relu)
layer0 = model.blocks[0]
x1 = torch.randn(1, 80, 300)
x2 = torch.randn(1, 80, 300) * 5 + 3
with torch.no_grad():
    y1 = layer0(x1)
    y2 = layer0(x2)
print(f"\n  Full layer0 test:")
print(f"  Input1: mean={x1.mean():.4f}, std={x1.std():.4f}")
print(f"  Output1: mean={y1.mean():.4f}, std={y1.std():.4f}")
print(f"  Input2: mean={x2.mean():.4f}, std={x2.std():.4f}")
print(f"  Output2: mean={y2.mean():.4f}, std={y2.std():.4f}")
cos = torch.nn.functional.cosine_similarity(y1.flatten().unsqueeze(0), y2.flatten().unsqueeze(0))
print(f"  Cosine(y1, y2): {cos.item():.6f}")

# Test the full model with random inputs
print(f"\n=== Full Model Random Input Test ===")
for i in range(3):
    x = torch.randn(1, 300, 80) * (i + 1)
    with torch.no_grad():
        emb, _ = model(x, debug=False)
    emb_np = emb.squeeze(0).numpy()
    emb_np = emb_np / (np.linalg.norm(emb_np) + 1e-10)
    print(f"  Random input {i} (scale={i+1}): emb[:5]={emb_np[:5]}")

# Check if the running_var is so large that BN essentially passes through a constant
print(f"\n=== BN Effective Gain Analysis ===")
for i, block in enumerate(model.blocks):
    if hasattr(block, 'norm'):
        bn = block.norm
        # Effective gain = weight / sqrt(running_var + eps)
        gain = bn.weight / torch.sqrt(bn.running_var + 1e-5)
        print(f"  blocks.{i}.norm: gain mean={gain.mean():.6f}, std={gain.std():.6f}, max={gain.max():.6f}")

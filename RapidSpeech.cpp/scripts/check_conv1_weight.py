"""Check conv1 weight values and shape in both safetensors and GGUF."""
import numpy as np
from pathlib import Path
from safetensors import safe_open

model_dir = Path("models/moonshine-tiny")

# From safetensors
with safe_open(str(model_dir / "model.safetensors"), framework="numpy") as f:
    w = f.get_tensor("model.encoder.conv1.weight")
    print(f"Safetensors conv1.weight: shape={w.shape}, dtype={w.dtype}")
    print(f"  min={w.min():.6f} max={w.max():.6f} mean={w.mean():.6f}")
    print(f"  [0,0,:5] = {w[0,0,:5]}")
    print(f"  sum of abs = {np.abs(w).sum():.6f}")

# From GGUF - read raw tensor
import struct
gguf_path = model_dir.parent / "gguf" / "moonshine-tiny.gguf"
if gguf_path.exists():
    # Use gguf library to read
    try:
        from gguf import GGUFReader
        reader = GGUFReader(str(gguf_path))
        for tensor in reader.tensors:
            if "conv1.weight" in tensor.name:
                data = tensor.data
                print(f"\nGGUF {tensor.name}: shape={tensor.shape}, type={tensor.tensor_type}")
                flat = data.flatten()
                print(f"  min={flat.min():.6f} max={flat.max():.6f} mean={flat.mean():.6f}")
                print(f"  first 5 = {flat[:5]}")
                print(f"  sum of abs = {np.abs(flat).sum():.6f}")
                # Check if shapes match
                print(f"  n_elements = {flat.shape[0]} (expected {288*1*127}={288*127})")
                break
    except Exception as e:
        print(f"Error reading GGUF: {e}")

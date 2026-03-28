"""Check if lm_head weight exists and differs from embed_tokens."""
import numpy as np
from pathlib import Path
from safetensors import safe_open

model_dir = Path("models/moonshine-tiny")
with safe_open(str(model_dir / "model.safetensors"), framework="numpy") as f:
    names = list(f.keys())
    print("All tensor names containing 'lm_head' or 'embed':")
    for n in names:
        if 'lm_head' in n or 'embed' in n:
            t = f.get_tensor(n)
            print(f"  {n}: shape={t.shape}, dtype={t.dtype}")
    
    # Check if lm_head exists
    has_lm_head = any('lm_head' in n for n in names)
    print(f"\nHas lm_head: {has_lm_head}")
    
    if has_lm_head:
        lm = f.get_tensor("lm_head.weight")
        emb = f.get_tensor("model.decoder.embed_tokens.weight")
        print(f"lm_head shape: {lm.shape}")
        print(f"embed_tokens shape: {emb.shape}")
        if lm.shape == emb.shape:
            diff = np.abs(lm - emb).max()
            print(f"Max diff between lm_head and embed_tokens: {diff}")
            print(f"Are they the same: {diff < 1e-6}")

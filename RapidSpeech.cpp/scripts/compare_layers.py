#!/usr/bin/env python3
"""
Compare ECAPA-TDNN intermediate layer outputs between PyTorch and C++ (ggml).

PyTorch dumps: .npy files (numpy)
C++ dumps: .bin (raw float32) + .shape (comma-separated dims)

The key difference: PyTorch uses [C, T] layout (channels first),
while ggml uses [T, C] layout (ne[0]=time, ne[1]=channels).
This script handles the transpose automatically.

Usage:
  python compare_layers.py [--pytorch <dir>] [--cpp <dir>]
"""

import argparse
import os
import sys
import numpy as np


def load_pytorch_tensor(path):
    """Load .npy tensor from PyTorch dump."""
    return np.load(path)


def load_cpp_tensor(bin_path, shape_path):
    """Load raw float32 binary + shape file from C++ dump.
    
    C++ ggml tensors are stored in ggml memory order:
    ne[0] is the innermost (contiguous) dimension.
    For a 2D tensor [T, C]: ne[0]=T, ne[1]=C
    The binary is stored as T*C floats in row-major order of ggml,
    which means the data is laid out as C groups of T values.
    """
    with open(shape_path, 'r') as f:
        dims = [int(x) for x in f.read().strip().split(',')]
    
    data = np.fromfile(bin_path, dtype=np.float32)
    
    # ggml stores data with ne[0] as innermost dim
    # For 2D: shape file has "T,C" (ne[0], ne[1])
    # Data layout: C blocks of T floats each
    # numpy reshape: data.reshape(C, T) gives [C, T] in row-major
    if len(dims) == 1:
        return data.reshape(dims[0])
    elif len(dims) == 2:
        # ggml [ne0, ne1] -> numpy [ne1, ne0] (row-major)
        return data.reshape(dims[1], dims[0])
    elif len(dims) == 3:
        return data.reshape(dims[2], dims[1], dims[0])
    else:
        return data.reshape(list(reversed(dims)))


def compare_tensor(name, pt, cpp, atol=1e-4, rtol=1e-3):
    """Compare two tensors and print diagnostics."""
    if pt.shape != cpp.shape:
        # Try squeezing trailing 1-dims (ggml [N,1] vs pytorch [N])
        pt_sq = pt.squeeze()
        cpp_sq = cpp.squeeze()
        if pt_sq.shape == cpp_sq.shape:
            pt = pt_sq
            cpp = cpp_sq
        elif len(pt.shape) == 2 and pt.shape == cpp.shape[::-1]:
            cpp = cpp.T
        else:
            print(f"  {name:40s} SHAPE MISMATCH: pytorch={pt.shape} cpp={cpp.shape}")
            return False
    
    diff = np.abs(pt - cpp)
    max_diff = diff.max()
    mean_diff = diff.mean()
    
    # Cosine similarity
    pt_flat = pt.flatten()
    cpp_flat = cpp.flatten()
    dot = np.dot(pt_flat, cpp_flat)
    norm_pt = np.linalg.norm(pt_flat)
    norm_cpp = np.linalg.norm(cpp_flat)
    cos_sim = dot / (norm_pt * norm_cpp + 1e-12)
    
    # Relative error
    rel_err = diff / (np.abs(pt) + 1e-8)
    max_rel = rel_err.max()
    
    ok = max_diff < atol or cos_sim > 0.9999
    status = "OK" if ok else "DIVERGED"
    
    print(f"  {name:40s} {status:8s} "
          f"max_diff={max_diff:10.6f} mean_diff={mean_diff:10.6f} "
          f"cos_sim={cos_sim:.8f} max_rel={max_rel:.6f}")
    
    if not ok:
        # Show where the biggest differences are
        flat_idx = np.argmax(diff.flatten())
        idx = np.unravel_index(flat_idx, diff.shape)
        print(f"  {'':40s}          "
              f"worst_at={idx} pt={pt[idx]:.6f} cpp={cpp[idx]:.6f}")
        
        # Show first few values
        print(f"  {'':40s}          "
              f"pt[:5]={pt_flat[:5]}")
        print(f"  {'':40s}          "
              f"cpp[:5]={cpp_flat[:5]}")
    
    return ok


# Layer names to compare (in order of forward pass)
LAYER_NAMES = [
    "layer0_out",
    "block1_out",
    "block2_out",
    "block3_out",
    "mfa_out",
    "asp_tdnn_out",
    "asp_softmax",
    "asp_pooled",
    "asp_bn_out",
    "fc_out",
]


def compare_fbank_inputs(pt_dir, cpp_dir):
    """Compare fbank inputs between PyTorch and C++.
    
    PyTorch: fbank_input.npy [T, n_mels]
    C++: fbank_input.bin + fbank_input.shape "T,n_mels" — stored as [T, n_mels] row-major
    """
    pt_path = os.path.join(pt_dir, "fbank_input.npy")
    cpp_bin = os.path.join(cpp_dir, "fbank_input.bin")
    cpp_shape = os.path.join(cpp_dir, "fbank_input.shape")

    if not os.path.exists(pt_path) or not os.path.exists(cpp_bin):
        print("  fbank_input: SKIP (missing dumps)")
        return

    pt = np.load(pt_path)  # [T, n_mels]

    with open(cpp_shape, 'r') as f:
        dims = [int(x) for x in f.read().strip().split(',')]
    cpp_raw = np.fromfile(cpp_bin, dtype=np.float32)
    # C++ now stores as [T, n_mels] row-major (same layout as PyTorch)
    cpp = cpp_raw.reshape(dims[0], dims[1])  # [T, n_mels]

    compare_tensor("fbank_input", pt, cpp, atol=1e-2)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--pytorch", type=str,
                        default=os.path.join(os.path.dirname(__file__),
                                             "..", "debug_dump", "pytorch"))
    parser.add_argument("--cpp", type=str,
                        default=os.path.join(os.path.dirname(__file__),
                                             "..", "debug_dump", "cpp"))
    parser.add_argument("--atol", type=float, default=1e-3)
    args = parser.parse_args()

    print(f"PyTorch dump dir: {args.pytorch}")
    print(f"C++ dump dir:     {args.cpp}")
    print(f"Tolerance:        atol={args.atol}")
    print()

    if not os.path.isdir(args.pytorch):
        print(f"ERROR: PyTorch dump dir not found: {args.pytorch}")
        sys.exit(1)
    if not os.path.isdir(args.cpp):
        print(f"ERROR: C++ dump dir not found: {args.cpp}")
        sys.exit(1)

    # First compare fbank inputs
    print("=== Fbank Input ===")
    compare_fbank_inputs(args.pytorch, args.cpp)
    print()

    print("=== Layer Outputs ===")
    first_diverged = None

    for name in LAYER_NAMES:
        pt_path = os.path.join(args.pytorch, f"{name}.npy")
        cpp_bin = os.path.join(args.cpp, f"{name}.bin")
        cpp_shape = os.path.join(args.cpp, f"{name}.shape")

        if not os.path.exists(pt_path):
            print(f"  {name:40s} SKIP (no pytorch dump)")
            continue
        if not os.path.exists(cpp_bin):
            print(f"  {name:40s} SKIP (no cpp dump)")
            continue

        pt = load_pytorch_tensor(pt_path)
        cpp = load_cpp_tensor(cpp_bin, cpp_shape)

        ok = compare_tensor(name, pt, cpp, atol=args.atol)
        if not ok and first_diverged is None:
            first_diverged = name

    print()
    if first_diverged:
        print(f"FIRST DIVERGENCE at: {first_diverged}")
        print("Focus debugging on this layer and its inputs.")
    else:
        print("All layers match within tolerance.")


if __name__ == "__main__":
    main()

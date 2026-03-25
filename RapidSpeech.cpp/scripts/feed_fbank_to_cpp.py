#!/usr/bin/env python3
"""
Feed the same fbank input to C++ ECAPA-TDNN (via rs-speaker-verify or server)
by creating a synthetic WAV that produces known fbank features.

Alternative approach: directly write fbank as binary and use a custom C++ tool.

For now, this script:
1. Loads the PyTorch-dumped fbank_input.npy
2. Creates a WAV file that the C++ AudioProcessor will process
3. OR: provides instructions for using ECAPA_DEBUG_DUMP env var

Usage:
  # Step 1: Run PyTorch dump first
  python dump_pytorch_layers.py --outdir ../debug_dump/pytorch

  # Step 2: Run C++ with debug dump enabled
  #   On Linux/Mac:
  #     ECAPA_DEBUG_DUMP=debug_dump/cpp ./build/bin/rs-speaker-verify \
  #       --model ecapa_tdnn.gguf --wav1 test.wav --wav2 test.wav
  #   On Windows:
  #     set ECAPA_DEBUG_DUMP=debug_dump\\cpp
  #     build\\bin\\rs-speaker-verify.exe --model ecapa_tdnn.gguf --wav1 test.wav --wav2 test.wav

  # Step 3: Compare
  python compare_layers.py --pytorch ../debug_dump/pytorch --cpp ../debug_dump/cpp

  # NOTE: The fbank features will differ between PyTorch and C++ because they
  # use different fbank implementations. The comparison script handles this by
  # comparing from layer0_out onwards. If layer0_out already diverges, the issue
  # is in the conv/BN implementation, not fbank.
  #
  # For exact input matching, use this script to inject identical fbank:
  python feed_fbank_to_cpp.py
"""

import os
import sys
import struct
import numpy as np

DUMP_DIR = os.path.join(os.path.dirname(__file__), "..", "debug_dump")
PT_DIR = os.path.join(DUMP_DIR, "pytorch")
CPP_DIR = os.path.join(DUMP_DIR, "cpp")


def create_fbank_binary(fbank_npy_path, output_path):
    """Convert PyTorch fbank .npy to raw binary that C++ can load directly.
    
    PyTorch fbank: [T, n_mels] (row-major)
    C++ Encode now expects [T, n_mels] row-major (transpose is done in ggml graph).
    """
    fbank = np.load(fbank_npy_path)  # [T, n_mels]
    T, n_mels = fbank.shape
    
    fbank_cpp = np.ascontiguousarray(fbank.astype(np.float32))
    
    with open(output_path, 'wb') as f:
        f.write(fbank_cpp.tobytes())
    
    print(f"Written fbank binary: {output_path}")
    print(f"  Shape: [{T}, {n_mels}] = {T * n_mels} floats")
    print(f"  Size: {os.path.getsize(output_path)} bytes")
    return n_mels, T


def main():
    fbank_path = os.path.join(PT_DIR, "fbank_input.npy")
    
    if not os.path.exists(fbank_path):
        print(f"PyTorch fbank dump not found: {fbank_path}")
        print("Run dump_pytorch_layers.py first.")
        sys.exit(1)
    
    os.makedirs(CPP_DIR, exist_ok=True)
    
    # Create binary fbank for C++ injection
    out_path = os.path.join(CPP_DIR, "fbank_inject.bin")
    n_mels, T = create_fbank_binary(fbank_path, out_path)
    
    print(f"\nTo use with C++ debug dump:")
    print(f"  1. Set env: ECAPA_DEBUG_DUMP={CPP_DIR}")
    print(f"  2. Run rs-speaker-verify with any WAV file")
    print(f"  3. The C++ code will dump its own fbank + layer outputs to {CPP_DIR}")
    print(f"  4. Run: python compare_layers.py")
    print(f"\nNote: fbank will differ (different implementations).")
    print(f"The comparison focuses on whether the model layers match given their respective inputs.")


if __name__ == "__main__":
    main()

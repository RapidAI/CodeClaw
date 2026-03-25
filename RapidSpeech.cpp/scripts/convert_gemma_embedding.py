#!/usr/bin/env python3
"""
Convert Gemma 300M embedding model (ONNX/HuggingFace) to GGUF format.

This is the embeddinggemma-300m model used by Moonshine for intent recognition.
Produces 768-dim text embeddings with MRL (Matryoshka) support.

Usage:
    python convert_gemma_embedding.py --model-dir path/to/gemma-emb --output gemma-emb.gguf
    python convert_gemma_embedding.py --model-dir path/to/gemma-emb --output gemma-emb-f16.gguf --out-type f16
"""

import argparse
import json
import numpy as np
from pathlib import Path

try:
    import torch
except ImportError:
    torch = None

try:
    from safetensors import safe_open
except ImportError:
    safe_open = None

from gguf import GGUFWriter, GGMLQuantizationType


def load_weights(model_dir: Path):
    """Load weights from pytorch_model.bin or model.safetensors."""
    pt_path = model_dir / "pytorch_model.bin"
    st_path = model_dir / "model.safetensors"

    if pt_path.exists() and torch is not None:
        print(f"Loading weights from {pt_path}")
        state_dict = torch.load(str(pt_path), map_location="cpu", weights_only=True)
        for name, tensor in state_dict.items():
            yield name, tensor.detach().float().numpy()
    elif st_path.exists() and safe_open is not None:
        print(f"Loading weights from {st_path}")
        with safe_open(str(st_path), framework="numpy") as f:
            for name in f.keys():
                yield name, f.get_tensor(name).astype(np.float32)
    else:
        raise FileNotFoundError(f"No weights found in {model_dir}")


def load_tokenizer(model_dir: Path):
    """Load tokenizer vocabulary."""
    tok_path = model_dir / "tokenizer.json"
    if not tok_path.exists():
        # Try tokenizer.bin (Moonshine format)
        tok_bin = model_dir / "tokenizer.bin"
        if tok_bin.exists():
            print(f"Warning: tokenizer.bin found but not supported, skipping vocab")
        return []

    with open(tok_path, "r", encoding="utf-8") as f:
        tok_data = json.load(f)

    vocab = tok_data.get("model", {}).get("vocab", {})
    if vocab:
        sorted_tokens = sorted(vocab.items(), key=lambda x: x[1])
        return [t[0] for t in sorted_tokens]
    return []


def main():
    parser = argparse.ArgumentParser(description="Convert Gemma embedding model to GGUF")
    parser.add_argument("--model-dir", type=str, required=True)
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--out-type", type=str, choices=["f32", "f16"], default="f32")
    parser.add_argument("--output-dim", type=int, default=768,
                        help="Output embedding dimension (MRL truncation: 128/256/512/768)")
    args = parser.parse_args()

    model_dir = Path(args.model_dir)
    ftype = GGMLQuantizationType.F32 if args.out_type == "f32" else GGMLQuantizationType.F16

    # Load config
    config_path = model_dir / "config.json"
    config = {}
    if config_path.exists():
        with open(config_path, "r") as f:
            config = json.load(f)

    writer = GGUFWriter(args.output, "gemma-embedding")

    # Write hyperparameters
    writer.add_int32("gemma.hidden_size", config.get("hidden_size", 768))
    writer.add_int32("gemma.num_hidden_layers", config.get("num_hidden_layers", 18))
    writer.add_int32("gemma.num_attention_heads", config.get("num_attention_heads", 12))
    writer.add_int32("gemma.head_dim", config.get("head_dim", 64))
    writer.add_int32("gemma.intermediate_size", config.get("intermediate_size", 3072))
    writer.add_int32("gemma.vocab_size", config.get("vocab_size", 256000))
    writer.add_int32("gemma.max_position_embeddings", config.get("max_position_embeddings", 512))
    writer.add_int32("gemma.output_dim", args.output_dim)
    writer.add_float32("gemma.rms_norm_eps", config.get("rms_norm_eps", 1e-6))
    writer.add_float32("gemma.rope_theta", config.get("rope_theta", 10000.0))

    # Vocabulary
    tokens = load_tokenizer(model_dir)
    if tokens:
        writer.add_int32("tokenizer.vocab_size", len(tokens))
        writer.add_token_list(tokens)
        print(f"Wrote {len(tokens)} vocabulary tokens")

    # Tensors
    print("Writing tensors...")
    count = 0
    for name, data in load_weights(model_dir):
        if ftype == GGMLQuantizationType.F16 and data.ndim >= 2:
            data = data.astype(np.float16)
        else:
            data = data.astype(np.float32)
        writer.add_tensor(name, data)
        count += 1

    print(f"Wrote {count} tensors")

    writer.write_header_to_file()
    writer.write_kv_data_to_file()
    writer.write_tensors_to_file()
    writer.close()

    print(f"Successfully exported to {args.output}")


if __name__ == "__main__":
    main()

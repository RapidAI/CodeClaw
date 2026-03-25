#!/usr/bin/env python3
"""
Convert SpeechBrain ECAPA-TDNN model to GGUF format for RapidSpeech.cpp

Usage:
  # Download checkpoint first (needs HF_ENDPOINT for China):
  #   HF_ENDPOINT=https://hf-mirror.com python -c "
  #     from huggingface_hub import hf_hub_download
  #     hf_hub_download('speechbrain/spkrec-ecapa-voxceleb', 'embedding_model.ckpt', local_dir='ecapa-raw')
  #   "
  # Then convert:
  python convert_ecapa_tdnn.py --ckpt ecapa-raw/embedding_model.ckpt --output ecapa_tdnn.gguf

Requirements:
  pip install torch numpy
"""

import argparse
import sys
import struct
import numpy as np


# GGUF constants
GGUF_MAGIC = 0x46554747  # "GGUF" in little-endian
GGUF_VERSION = 3
GGML_TYPE_F32 = 0
GGML_TYPE_F16 = 1


class GGUFWriter:
    """Minimal GGUF writer for ECAPA-TDNN conversion."""

    def __init__(self):
        self.kv_data = []
        self.tensors = []  # (name, shape, dtype, data_bytes)

    def add_string(self, key: str, value: str):
        self.kv_data.append(("string", key, value))

    def add_int32(self, key: str, value: int):
        self.kv_data.append(("int32", key, value))

    def _write_string(self, f, s: str):
        encoded = s.encode("utf-8")
        f.write(struct.pack("<Q", len(encoded)))
        f.write(encoded)

    def add_tensor(self, name: str, data: np.ndarray, dtype=GGML_TYPE_F32):
        if dtype == GGML_TYPE_F16:
            data = data.astype(np.float16)
        else:
            data = data.astype(np.float32)
        # ggml uses column-major layout: ne[0] is the innermost (contiguous) dimension.
        # numpy uses row-major: the last axis is contiguous.
        # So ggml ne[] = reversed numpy shape, and we store data in numpy C-order
        # which naturally gives ggml the correct memory layout.
        ggml_shape = list(reversed(data.shape))
        self.tensors.append((name, ggml_shape, dtype, data.tobytes()))

    def write(self, path: str):
        with open(path, "wb") as f:
            # Header
            f.write(struct.pack("<I", GGUF_MAGIC))
            f.write(struct.pack("<I", GGUF_VERSION))
            f.write(struct.pack("<Q", len(self.tensors)))
            f.write(struct.pack("<Q", len(self.kv_data)))

            # KV pairs
            for kv in self.kv_data:
                if kv[0] == "string":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 8))  # GGUF_TYPE_STRING
                    self._write_string(f, kv[2])
                elif kv[0] == "int32":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 5))  # GGUF_TYPE_INT32
                    f.write(struct.pack("<i", kv[2]))

            # Tensor infos
            data_offset = 0
            tensor_offsets = []
            for name, shape, dtype, data_bytes in self.tensors:
                self._write_string(f, name)
                n_dims = len(shape)
                f.write(struct.pack("<I", n_dims))
                for dim in shape:
                    f.write(struct.pack("<Q", dim))
                f.write(struct.pack("<I", dtype))
                # Align offset to 32 bytes
                aligned = (data_offset + 31) & ~31
                f.write(struct.pack("<Q", aligned))
                tensor_offsets.append(aligned)
                data_offset = aligned + len(data_bytes)

            # Padding to align data start
            current_pos = f.tell()
            aligned_start = (current_pos + 31) & ~31
            f.write(b"\x00" * (aligned_start - current_pos))

            data_base = f.tell()

            # Tensor data
            for i, (name, shape, dtype, data_bytes) in enumerate(self.tensors):
                target_pos = data_base + tensor_offsets[i]
                current = f.tell()
                if current < target_pos:
                    f.write(b"\x00" * (target_pos - current))
                f.write(data_bytes)

        print(f"Written {path} ({len(self.tensors)} tensors)")


def convert_tdnn_block(writer, sd, src_prefix, dst_prefix, use_f16=False):
    """Convert a TDNNBlock: Conv1d + BatchNorm1d + ReLU.

    SpeechBrain checkpoint keys use double-nested names like:
      {prefix}.conv.conv.weight  and  {prefix}.norm.norm.weight
    We flatten to:
      {dst_prefix}.conv.weight  and  {dst_prefix}.norm.weight
    """
    dtype = GGML_TYPE_F16 if use_f16 else GGML_TYPE_F32

    # Conv1d weight: PyTorch [OC, IC, K] — ggml ne = [K, IC, OC]
    # add_tensor handles the shape reversal automatically
    w = sd[f"{src_prefix}.conv.conv.weight"].cpu().numpy()
    writer.add_tensor(f"{dst_prefix}.conv.weight", w, dtype)
    b = sd[f"{src_prefix}.conv.conv.bias"].cpu().numpy()
    writer.add_tensor(f"{dst_prefix}.conv.bias", b, GGML_TYPE_F32)

    writer.add_tensor(f"{dst_prefix}.norm.weight",
                      sd[f"{src_prefix}.norm.norm.weight"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor(f"{dst_prefix}.norm.bias",
                      sd[f"{src_prefix}.norm.norm.bias"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor(f"{dst_prefix}.norm.running_mean",
                      sd[f"{src_prefix}.norm.norm.running_mean"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor(f"{dst_prefix}.norm.running_var",
                      sd[f"{src_prefix}.norm.norm.running_var"].cpu().numpy(), GGML_TYPE_F32)


def convert_se_res2_block(writer, sd, src_prefix, dst_prefix, n_res2_convs,
                          has_shortcut, use_f16=False):
    """Convert an SE-Res2Block.

    SpeechBrain structure:
      {prefix}.tdnn1  (TDNNBlock)
      {prefix}.res2net_block.blocks.{i}  (TDNNBlock, i=0..scale-2)
      {prefix}.tdnn2  (TDNNBlock)
      {prefix}.se_block.conv1  (Conv1d with .conv sub-key)
      {prefix}.se_block.conv2  (Conv1d with .conv sub-key)
    """
    dtype = GGML_TYPE_F16 if use_f16 else GGML_TYPE_F32

    # tdnn1
    convert_tdnn_block(writer, sd, f"{src_prefix}.tdnn1", f"{dst_prefix}.tdnn1", use_f16)

    # res2net sub-band convolutions
    for i in range(n_res2_convs):
        convert_tdnn_block(writer, sd,
                           f"{src_prefix}.res2net_block.blocks.{i}",
                           f"{dst_prefix}.res2_convs.{i}", use_f16)

    # tdnn2
    convert_tdnn_block(writer, sd, f"{src_prefix}.tdnn2", f"{dst_prefix}.tdnn2", use_f16)

    # SE block: Conv1d weights [OC, IC, 1] — ggml ne = [1, IC, OC]
    for layer in ["conv1", "conv2"]:
        w = sd[f"{src_prefix}.se_block.{layer}.conv.weight"].cpu().numpy()
        writer.add_tensor(f"{dst_prefix}.se.{layer}.weight", w, dtype)
        b = sd[f"{src_prefix}.se_block.{layer}.conv.bias"].cpu().numpy()
        writer.add_tensor(f"{dst_prefix}.se.{layer}.bias", b, GGML_TYPE_F32)

    # Shortcut (if present): bare Conv1d [OC, IC, 1] — ggml ne = [1, IC, OC]
    if has_shortcut:
        w = sd[f"{src_prefix}.shortcut.conv.weight"].cpu().numpy()
        writer.add_tensor(f"{dst_prefix}.shortcut.conv.weight", w, dtype)
        b = sd[f"{src_prefix}.shortcut.conv.bias"].cpu().numpy()
        writer.add_tensor(f"{dst_prefix}.shortcut.conv.bias", b, GGML_TYPE_F32)


def main():
    parser = argparse.ArgumentParser(description="Convert ECAPA-TDNN to GGUF")
    parser.add_argument("--ckpt", type=str, required=True,
                        help="Path to embedding_model.ckpt")
    parser.add_argument("--output", type=str, default="ecapa_tdnn.gguf",
                        help="Output GGUF file path")
    parser.add_argument("--f16", action="store_true",
                        help="Store conv weights in float16")
    args = parser.parse_args()

    print(f"Loading checkpoint: {args.ckpt}")

    import torch
    sd = torch.load(args.ckpt, map_location="cpu")

    print(f"Checkpoint has {len(sd)} parameters")

    # Detect hyperparameters from weight shapes
    layer0_w = sd["blocks.0.conv.conv.weight"]  # [C, n_mels, kernel]
    channels = int(layer0_w.shape[0])  # 1024
    n_mels = int(layer0_w.shape[1])    # 80

    # Detect res2_scale from number of res2net sub-convolutions in block 1
    n_res2_convs = 0
    while f"blocks.1.res2net_block.blocks.{n_res2_convs}.conv.conv.weight" in sd:
        n_res2_convs += 1
    res2_scale = n_res2_convs + 1  # +1 for identity sub-band

    # Detect embedding dim from FC
    fc_w = sd["fc.conv.weight"]  # [emb_dim, C*2, 1]
    emb_dim = int(fc_w.shape[0])  # 192

    # ASP attention channels from asp.tdnn
    asp_tdnn_w = sd["asp.tdnn.conv.conv.weight"]  # [attn_ch, C*3, 1]
    attn_channels = int(asp_tdnn_w.shape[0])  # 128

    print(f"Detected: n_mels={n_mels}, channels={channels}, emb_dim={emb_dim}, "
          f"res2_scale={res2_scale}, attn_channels={attn_channels}")

    writer = GGUFWriter()

    # Metadata
    writer.add_string("general.architecture", "ecapa-tdnn")
    writer.add_string("general.name", "ECAPA-TDNN Speaker Embedding")
    writer.add_int32("ecapa.n_mels", n_mels)
    writer.add_int32("ecapa.channels", channels)
    writer.add_int32("ecapa.emb_dim", emb_dim)
    writer.add_int32("ecapa.res2_scale", res2_scale)
    writer.add_int32("ecapa.attn_channels", attn_channels)

    use_f16 = args.f16
    dtype = GGML_TYPE_F16 if use_f16 else GGML_TYPE_F32

    # Layer 0: initial TDNN
    convert_tdnn_block(writer, sd, "blocks.0", "blocks.0", use_f16)

    # SE-Res2Blocks (blocks.1, blocks.2, blocks.3)
    for i in range(3):
        src = f"blocks.{i + 1}"
        dst = f"blocks.{i + 1}"
        has_shortcut = f"{src}.shortcut.conv.weight" in sd
        convert_se_res2_block(writer, sd, src, dst, n_res2_convs,
                              has_shortcut, use_f16)

    # MFA conv
    convert_tdnn_block(writer, sd, "mfa", "mfa", use_f16)

    # ASP (Attentive Statistical Pooling)
    # asp.tdnn is a TDNNBlock (Conv1d + BN + ReLU)
    convert_tdnn_block(writer, sd, "asp.tdnn", "asp.tdnn", use_f16)

    # asp.conv is a bare Conv1d: [channels, attn_ch, 1] — ggml ne = [1, attn_ch, channels]
    w = sd["asp.conv.conv.weight"].cpu().numpy()
    writer.add_tensor("asp.conv.weight", w, dtype)
    b = sd["asp.conv.conv.bias"].cpu().numpy()
    writer.add_tensor("asp.conv.bias", b, GGML_TYPE_F32)

    # asp_bn: BatchNorm1d on pooled output (channels*2)
    writer.add_tensor("asp_bn.weight",
                      sd["asp_bn.norm.weight"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor("asp_bn.bias",
                      sd["asp_bn.norm.bias"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor("asp_bn.running_mean",
                      sd["asp_bn.norm.running_mean"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor("asp_bn.running_var",
                      sd["asp_bn.norm.running_var"].cpu().numpy(), GGML_TYPE_F32)

    # Final FC: Conv1d [emb_dim, C*2, 1] — ggml ne = [1, C*2, emb_dim]
    w = sd["fc.conv.weight"].cpu().numpy()
    writer.add_tensor("fc.weight", w, dtype)
    b = sd["fc.conv.bias"].cpu().numpy()
    writer.add_tensor("fc.bias", b, GGML_TYPE_F32)

    writer.write(args.output)
    print(f"Done! Output: {args.output}")


if __name__ == "__main__":
    main()

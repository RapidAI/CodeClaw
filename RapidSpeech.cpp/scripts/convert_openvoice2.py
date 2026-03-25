#!/usr/bin/env python3
"""
Convert OpenVoice2 (MeloTTS base + Tone Color Converter) to GGUF format.

Produces two files:
  - openvoice2-base.gguf    (text encoder + duration predictor + flow decoder + vocoder)
  - openvoice2-converter.gguf (tone color converter, optional)

Usage:
  python convert_openvoice2.py \
    --base-model myshell-ai/MeloTTS-English \
    --converter-model myshell-ai/OpenVoiceV2 \
    --output-dir ./models \
    [--f16] [--q8]

Requirements:
  pip install torch numpy openvoice melo-tts
"""

import argparse
import sys
import struct
import numpy as np

# GGUF constants
GGUF_MAGIC = 0x46475547  # "GGUF"
GGUF_VERSION = 3
GGML_TYPE_F32 = 0
GGML_TYPE_F16 = 1
GGML_TYPE_Q8_0 = 8


class GGUFWriter:
    """Minimal GGUF writer (same pattern as convert_ecapa_tdnn.py)."""

    def __init__(self):
        self.kv_data = []
        self.tensors = []

    def add_string(self, key: str, value: str):
        self.kv_data.append(("string", key, value))

    def add_int32(self, key: str, value: int):
        self.kv_data.append(("int32", key, value))

    def add_float32(self, key: str, value: float):
        self.kv_data.append(("float32", key, value))

    def _write_string(self, f, s: str):
        encoded = s.encode("utf-8")
        f.write(struct.pack("<Q", len(encoded)))
        f.write(encoded)

    def add_tensor(self, name: str, data: np.ndarray, dtype=GGML_TYPE_F32):
        if dtype == GGML_TYPE_F16:
            data = data.astype(np.float16)
        elif dtype == GGML_TYPE_Q8_0:
            # Simplified Q8_0: quantize to int8 with per-block scale
            data = _quantize_q8_0(data.astype(np.float32))
        else:
            data = data.astype(np.float32)
        self.tensors.append((name, list(data.shape), dtype, data.tobytes()))

    def write(self, path: str):
        with open(path, "wb") as f:
            f.write(struct.pack("<I", GGUF_MAGIC))
            f.write(struct.pack("<I", GGUF_VERSION))
            f.write(struct.pack("<Q", len(self.tensors)))
            f.write(struct.pack("<Q", len(self.kv_data)))

            for kv in self.kv_data:
                if kv[0] == "string":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 8))  # GGUF_TYPE_STRING
                    self._write_string(f, kv[2])
                elif kv[0] == "int32":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 4))  # GGUF_TYPE_INT32
                    f.write(struct.pack("<i", kv[2]))
                elif kv[0] == "float32":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 6))  # GGUF_TYPE_FLOAT32
                    f.write(struct.pack("<f", kv[2]))

            data_offset = 0
            tensor_offsets = []
            for name, shape, dtype, data_bytes in self.tensors:
                self._write_string(f, name)
                n_dims = len(shape)
                f.write(struct.pack("<I", n_dims))
                for dim in shape:
                    f.write(struct.pack("<Q", dim))
                f.write(struct.pack("<I", dtype))
                aligned = (data_offset + 31) & ~31
                f.write(struct.pack("<Q", aligned))
                tensor_offsets.append(aligned)
                data_offset = aligned + len(data_bytes)

            current_pos = f.tell()
            aligned_start = (current_pos + 31) & ~31
            f.write(b"\x00" * (aligned_start - current_pos))
            data_base = f.tell()

            for i, (name, shape, dtype, data_bytes) in enumerate(self.tensors):
                target_pos = data_base + tensor_offsets[i]
                current = f.tell()
                if current < target_pos:
                    f.write(b"\x00" * (target_pos - current))
                f.write(data_bytes)

        print(f"Written {path} ({len(self.tensors)} tensors)")


def _quantize_q8_0(data: np.ndarray) -> np.ndarray:
    """Placeholder Q8_0 quantization — for now just store as F32.
    Real Q8_0 needs block-wise quantization matching ggml format."""
    # TODO: implement proper Q8_0 block quantization
    return data


def _get_dtype(use_f16: bool, use_q8: bool):
    if use_q8:
        return GGML_TYPE_Q8_0
    elif use_f16:
        return GGML_TYPE_F16
    return GGML_TYPE_F32


def convert_linear(writer, state_dict, src, dst, dtype):
    """Convert a nn.Linear layer."""
    w = state_dict[f"{src}.weight"].cpu().numpy()
    writer.add_tensor(f"{dst}.weight", w, dtype)
    bias_key = f"{src}.bias"
    if bias_key in state_dict:
        writer.add_tensor(f"{dst}.bias",
                          state_dict[bias_key].cpu().numpy(), GGML_TYPE_F32)


def convert_conv1d(writer, state_dict, src, dst, dtype):
    """Convert a nn.Conv1d layer."""
    w = state_dict[f"{src}.weight"].cpu().numpy()
    writer.add_tensor(f"{dst}.weight", w, dtype)
    bias_key = f"{src}.bias"
    if bias_key in state_dict:
        writer.add_tensor(f"{dst}.bias",
                          state_dict[bias_key].cpu().numpy(), GGML_TYPE_F32)


def convert_layer_norm(writer, state_dict, src, dst):
    """Convert a LayerNorm."""
    writer.add_tensor(f"{dst}.weight",
                      state_dict[f"{src}.weight"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor(f"{dst}.bias",
                      state_dict[f"{src}.bias"].cpu().numpy(), GGML_TYPE_F32)


def convert_base_tts(writer, state_dict, dtype):
    """Convert MeloTTS/VITS base model weights."""
    converted = 0

    # Text Encoder
    for k, v in state_dict.items():
        if k.startswith("enc_p."):
            name = k.replace("enc_p.", "text_encoder.")
            writer.add_tensor(name, v.cpu().numpy(),
                              dtype if "weight" in k and v.ndim >= 2 else GGML_TYPE_F32)
            converted += 1

    # Duration Predictor
    for k, v in state_dict.items():
        if k.startswith("dp."):
            name = k.replace("dp.", "duration_predictor.")
            writer.add_tensor(name, v.cpu().numpy(),
                              dtype if "weight" in k and v.ndim >= 2 else GGML_TYPE_F32)
            converted += 1

    # Flow Decoder
    for k, v in state_dict.items():
        if k.startswith("flow."):
            name = k.replace("flow.", "flow_decoder.")
            writer.add_tensor(name, v.cpu().numpy(),
                              dtype if "weight" in k and v.ndim >= 2 else GGML_TYPE_F32)
            converted += 1

    # Posterior Encoder (for training, but needed for full VITS)
    for k, v in state_dict.items():
        if k.startswith("enc_q."):
            name = k.replace("enc_q.", "posterior_encoder.")
            writer.add_tensor(name, v.cpu().numpy(),
                              dtype if "weight" in k and v.ndim >= 2 else GGML_TYPE_F32)
            converted += 1

    # HiFi-GAN Decoder (vocoder)
    for k, v in state_dict.items():
        if k.startswith("dec."):
            name = k.replace("dec.", "vocoder.")
            writer.add_tensor(name, v.cpu().numpy(),
                              dtype if "weight" in k and v.ndim >= 2 else GGML_TYPE_F32)
            converted += 1

    # Embedding tables
    for k, v in state_dict.items():
        if k.startswith("emb_"):
            writer.add_tensor(k, v.cpu().numpy(), GGML_TYPE_F32)
            converted += 1

    return converted


def convert_tone_converter(writer, state_dict, dtype):
    """Convert OpenVoice2 Tone Color Converter weights."""
    converted = 0
    for k, v in state_dict.items():
        writer.add_tensor(k, v.cpu().numpy(),
                          dtype if "weight" in k and v.ndim >= 2 else GGML_TYPE_F32)
        converted += 1
    return converted


def main():
    parser = argparse.ArgumentParser(description="Convert OpenVoice2 to GGUF")
    parser.add_argument("--base-model", type=str, required=True,
                        help="MeloTTS model path or HuggingFace ID")
    parser.add_argument("--converter-model", type=str, default=None,
                        help="OpenVoice2 tone color converter path (optional)")
    parser.add_argument("--output-dir", type=str, default=".",
                        help="Output directory for GGUF files")
    parser.add_argument("--f16", action="store_true",
                        help="Store large weights in float16")
    parser.add_argument("--q8", action="store_true",
                        help="Store large weights in Q8_0 (experimental)")
    parser.add_argument("--language", type=str, default="EN",
                        help="Language for MeloTTS (EN, ZH, etc.)")
    args = parser.parse_args()

    import os
    os.makedirs(args.output_dir, exist_ok=True)

    dtype = _get_dtype(args.f16, args.q8)
    dtype_name = {GGML_TYPE_F32: "F32", GGML_TYPE_F16: "F16", GGML_TYPE_Q8_0: "Q8_0"}[dtype]

    # --- Convert Base TTS ---
    print(f"Loading base TTS model: {args.base_model}")
    try:
        import torch
        from melo.api import TTS as MeloTTS
    except ImportError:
        print("Error: please install melo-tts and torch")
        print("  pip install melo-tts torch")
        sys.exit(1)

    tts = MeloTTS(language=args.language, device="cpu")
    base_sd = tts.model.state_dict()

    print(f"Base model has {len(base_sd)} parameters:")
    total_params = sum(v.numel() for v in base_sd.values())
    print(f"  Total parameters: {total_params:,}")

    writer = GGUFWriter()
    writer.add_string("general.architecture", "openvoice2")
    writer.add_string("general.name", "OpenVoice2 TTS")
    writer.add_string("general.language", args.language)

    # Detect model config from weights
    # VITS text encoder hidden channels
    if "enc_p.encoder.attn_layers.0.conv_k.weight" in base_sd:
        hidden_channels = base_sd["enc_p.encoder.attn_layers.0.conv_k.weight"].shape[1]
        writer.add_int32("openvoice2.hidden_channels", int(hidden_channels))

    # Vocoder upsample rates (from HiFi-GAN config)
    writer.add_int32("openvoice2.sample_rate", 22050)
    writer.add_int32("openvoice2.hop_length", 256)
    writer.add_int32("openvoice2.n_fft", 1024)

    n_converted = convert_base_tts(writer, base_sd, dtype)
    print(f"Converted {n_converted} tensors (dtype={dtype_name})")

    base_path = os.path.join(args.output_dir, "openvoice2-base.gguf")
    writer.write(base_path)

    # --- Convert Tone Color Converter (optional) ---
    if args.converter_model:
        print(f"\nLoading tone color converter: {args.converter_model}")
        try:
            from openvoice.api import ToneColorConverter
        except ImportError:
            print("Error: please install openvoice")
            print("  pip install openvoice")
            sys.exit(1)

        converter = ToneColorConverter(f"{args.converter_model}/converter")
        conv_sd = converter.model.state_dict()

        print(f"Converter model has {len(conv_sd)} parameters:")
        total_conv = sum(v.numel() for v in conv_sd.values())
        print(f"  Total parameters: {total_conv:,}")

        conv_writer = GGUFWriter()
        conv_writer.add_string("general.architecture", "openvoice2-converter")
        conv_writer.add_string("general.name", "OpenVoice2 Tone Color Converter")

        n_conv = convert_tone_converter(conv_writer, conv_sd, dtype)
        print(f"Converted {n_conv} tensors (dtype={dtype_name})")

        conv_path = os.path.join(args.output_dir, "openvoice2-converter.gguf")
        conv_writer.write(conv_path)

    print("\nDone!")


if __name__ == "__main__":
    main()

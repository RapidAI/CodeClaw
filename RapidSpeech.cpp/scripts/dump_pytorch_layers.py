#!/usr/bin/env python3
"""
Dump ECAPA-TDNN intermediate layer outputs from PyTorch reference.
Saves each layer's output as .npy for comparison with C++ ggml implementation.

Usage:
  python dump_pytorch_layers.py [--wav <path>] [--ckpt <path>] [--outdir <path>]

If no --wav is given, generates a deterministic synthetic fbank input.
"""

import argparse
import os
import sys
import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F

sys.path.insert(0, os.path.dirname(__file__))
from debug_ecapa_pytorch import (
    ECAPA_TDNN, load_speechbrain_ckpt,
    compute_fbank_pytorch, compute_fbank_pytorch_from_array,
)

DUMP_DIR = os.path.join(os.path.dirname(__file__), "..", "debug_dump", "pytorch")


def dump_tensor(name: str, t: torch.Tensor, outdir: str):
    """Save tensor as .npy with metadata printed."""
    arr = t.detach().cpu().numpy()
    path = os.path.join(outdir, f"{name}.npy")
    np.save(path, arr)
    print(f"  {name:40s} shape={str(list(arr.shape)):20s} "
          f"mean={arr.mean():12.6f} std={arr.std():12.6f} "
          f"min={arr.min():12.6f} max={arr.max():12.6f}")


def forward_with_dump(model: ECAPA_TDNN, fbank: np.ndarray, outdir: str):
    """Run forward pass layer by layer, dumping intermediates.
    
    fbank: [T, n_mels] numpy array
    """
    os.makedirs(outdir, exist_ok=True)
    model.eval()

    # Save input fbank
    np.save(os.path.join(outdir, "fbank_input.npy"), fbank)
    print(f"  {'fbank_input':40s} shape={str(list(fbank.shape)):20s} "
          f"mean={fbank.mean():12.6f} std={fbank.std():12.6f}")

    # [1, T, n_mels]
    x = torch.from_numpy(fbank).unsqueeze(0).float()
    # Transpose to [1, n_mels, T] for Conv1d
    x = x.transpose(1, 2)
    dump_tensor("input_transposed", x.squeeze(0), outdir)  # [n_mels, T]

    with torch.no_grad():
        # === Layer 0: TDNNBlock(80, 1024, 5) ===
        x = model.blocks[0](x)
        dump_tensor("layer0_out", x.squeeze(0), outdir)  # [C=1024, T]

        # === SE-Res2Blocks ===
        block_outs = []
        for bi in range(1, 4):
            block = model.blocks[bi]
            residual = x

            # tdnn1
            out = block.tdnn1(x)
            dump_tensor(f"block{bi}_tdnn1", out.squeeze(0), outdir)

            # res2net
            chunks = torch.chunk(out, block.res2net_block.scale, dim=1)
            sub_outs = [chunks[0]]
            for si in range(1, block.res2net_block.scale):
                inp = chunks[si] if si == 1 else chunks[si] + sub_outs[-1]
                sub_out = block.res2net_block.blocks[si - 1](inp)
                sub_outs.append(sub_out)
            # Dump first and last sub-band for debugging
            dump_tensor(f"block{bi}_res2_sub0", sub_outs[0].squeeze(0), outdir)
            dump_tensor(f"block{bi}_res2_sub{block.res2net_block.scale-1}",
                        sub_outs[-1].squeeze(0), outdir)
            out = torch.cat(sub_outs, dim=1)
            dump_tensor(f"block{bi}_res2_cat", out.squeeze(0), outdir)

            # tdnn2
            out = block.tdnn2(out)
            dump_tensor(f"block{bi}_tdnn2", out.squeeze(0), outdir)

            # SE
            out = block.se_block(out)
            dump_tensor(f"block{bi}_se", out.squeeze(0), outdir)

            # Residual
            if block.shortcut is not None:
                residual = block.shortcut(residual)
            out = out + residual
            dump_tensor(f"block{bi}_out", out.squeeze(0), outdir)

            x = out
            block_outs.append(x)

        # === MFA ===
        mfa_in = torch.cat(block_outs, dim=1)
        dump_tensor("mfa_cat", mfa_in.squeeze(0), outdir)
        x = model.mfa(mfa_in)
        dump_tensor("mfa_out", x.squeeze(0), outdir)  # [C*3=3072, T]


        # === ASP (Attentive Statistical Pooling) with global_context ===
        # x: [1, C*3, T]
        C3 = x.shape[1]
        T_out = x.shape[2]

        # Global context
        g_mean = x.mean(dim=2, keepdim=True).expand_as(x)
        g_std = x.std(dim=2, keepdim=True).expand_as(x)
        dump_tensor("asp_global_mean", g_mean.squeeze(0)[:, :1], outdir)  # [C*3, 1]
        dump_tensor("asp_global_std", g_std.squeeze(0)[:, :1], outdir)

        attn_in = torch.cat([x, g_mean, g_std], dim=1)  # [1, C*9, T]
        dump_tensor("asp_attn_input", attn_in.squeeze(0), outdir)

        # ASP tdnn
        attn = model.asp.tdnn(attn_in)
        dump_tensor("asp_tdnn_out", attn.squeeze(0), outdir)  # [1, 128, T]

        # ASP conv
        attn = model.asp.conv(attn)
        dump_tensor("asp_conv_out", attn.squeeze(0), outdir)  # [1, C*3, T]

        # Softmax over time (dim=2)
        attn = F.softmax(attn, dim=2)
        dump_tensor("asp_softmax", attn.squeeze(0), outdir)

        # Weighted mean and std
        w_mean = (x * attn).sum(dim=2)  # [1, C*3]
        w_sq = ((x ** 2) * attn).sum(dim=2)
        w_std = torch.sqrt(w_sq - w_mean ** 2 + 1e-12)
        dump_tensor("asp_w_mean", w_mean.squeeze(0), outdir)
        dump_tensor("asp_w_std", w_std.squeeze(0), outdir)

        pooled = torch.cat([w_mean, w_std], dim=1)  # [1, C*6]
        dump_tensor("asp_pooled", pooled.squeeze(0), outdir)

        # ASP BN
        pooled_bn = model.asp_bn(pooled)
        dump_tensor("asp_bn_out", pooled_bn.squeeze(0), outdir)

        # FC
        fc_in = pooled_bn.unsqueeze(2)  # [1, C*6, 1]
        emb = model.fc(fc_in).squeeze(2)  # [1, emb_dim]
        dump_tensor("fc_out", emb.squeeze(0), outdir)

        # L2 normalize
        emb_np = emb.squeeze(0).numpy()
        norm = np.linalg.norm(emb_np)
        emb_normed = emb_np / (norm + 1e-10)
        np.save(os.path.join(outdir, "embedding_normed.npy"), emb_normed)
        print(f"  {'embedding_normed':40s} shape={str(list(emb_normed.shape)):20s} "
              f"norm_before={norm:.6f}")

    print(f"\nAll tensors saved to: {outdir}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--wav", type=str, default=None)
    parser.add_argument("--ckpt", type=str, default=None)
    parser.add_argument("--outdir", type=str, default=DUMP_DIR)
    args = parser.parse_args()

    if args.ckpt is None:
        args.ckpt = os.path.join(os.path.dirname(__file__),
                                  "..", "..", "ecapa-raw", "embedding_model.ckpt")

    if not os.path.exists(args.ckpt):
        print(f"Checkpoint not found: {args.ckpt}")
        sys.exit(1)

    print(f"Loading model from: {args.ckpt}")
    model = ECAPA_TDNN(n_mels=80, channels=1024, emb_dim=192, res2_scale=8)
    model = load_speechbrain_ckpt(model, args.ckpt)
    model.eval()

    if args.wav:
        print(f"Computing fbank from: {args.wav}")
        fbank = compute_fbank_pytorch(args.wav)
    else:
        # Deterministic synthetic input: 3 seconds of mixed sine waves
        print("Using deterministic synthetic fbank input")
        np.random.seed(42)
        T = 300  # ~3 seconds
        fbank = np.random.randn(T, 80).astype(np.float32) * 2.0
        # Make it look like real fbank (values typically -5 to 15)
        fbank = fbank + 5.0

    if fbank is None:
        print("Failed to compute fbank")
        sys.exit(1)

    print(f"\nFbank shape: {fbank.shape}")
    print(f"Dumping layer outputs...\n")
    forward_with_dump(model, fbank, args.outdir)


if __name__ == "__main__":
    main()

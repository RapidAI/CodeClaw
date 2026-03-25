#!/usr/bin/env python3
"""
PyTorch reference forward pass for ECAPA-TDNN debugging.
Loads the SpeechBrain checkpoint directly (without SpeechBrain dependency),
runs forward pass on a WAV file, and dumps intermediate outputs.

This helps identify where the C++ ggml implementation diverges.
"""

import sys
import os
import struct
import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F


# =====================================================================
# Minimal ECAPA-TDNN implementation (matching SpeechBrain architecture)
# =====================================================================

class TDNNBlock(nn.Module):
    def __init__(self, in_ch, out_ch, kernel_size, dilation=1):
        super().__init__()
        self.conv = nn.Conv1d(in_ch, out_ch, kernel_size, dilation=dilation,
                              padding=(kernel_size - 1) * dilation // 2)
        self.norm = nn.BatchNorm1d(out_ch)
        self.activation = nn.ReLU()

    def forward(self, x):
        return self.activation(self.norm(self.conv(x)))


class SEBlock(nn.Module):
    def __init__(self, channels, se_channels=128):
        super().__init__()
        self.conv1 = nn.Conv1d(channels, se_channels, 1)
        self.conv2 = nn.Conv1d(se_channels, channels, 1)

    def forward(self, x):
        s = x.mean(dim=2, keepdim=True)  # [B, C, 1]
        s = F.relu(self.conv1(s))
        s = torch.sigmoid(self.conv2(s))
        return x * s


class Res2NetBlock(nn.Module):
    def __init__(self, channels, scale=8, kernel_size=3, dilation=1):
        super().__init__()
        self.scale = scale
        self.width = channels // scale
        self.blocks = nn.ModuleList([
            TDNNBlock(self.width, self.width, kernel_size, dilation)
            for _ in range(scale - 1)
        ])

    def forward(self, x):
        # x: [B, C, T]
        chunks = torch.chunk(x, self.scale, dim=1)
        y = [chunks[0]]
        for i in range(1, self.scale):
            if i == 1:
                inp = chunks[i]
            else:
                inp = chunks[i] + y[-1]
            y.append(self.blocks[i - 1](inp))
        return torch.cat(y, dim=1)


class SERes2Block(nn.Module):
    def __init__(self, in_ch, out_ch, res2_scale=8, kernel_size=3, dilation=1):
        super().__init__()
        self.tdnn1 = TDNNBlock(in_ch, out_ch, 1)
        self.res2net_block = Res2NetBlock(out_ch, res2_scale, kernel_size, dilation)
        self.tdnn2 = TDNNBlock(out_ch, out_ch, 1)
        self.se_block = SEBlock(out_ch)
        self.shortcut = None
        if in_ch != out_ch:
            self.shortcut = nn.Conv1d(in_ch, out_ch, 1)

    def forward(self, x):
        residual = x
        out = self.tdnn1(x)
        out = self.res2net_block(out)
        out = self.tdnn2(out)
        out = self.se_block(out)
        if self.shortcut is not None:
            residual = self.shortcut(x)
        return out + residual


class AttentiveStatisticsPooling(nn.Module):
    def __init__(self, channels, attn_channels=128, global_context=True):
        super().__init__()
        self.global_context = global_context
        in_ch = channels * 3 if global_context else channels
        self.tdnn = TDNNBlock(in_ch, attn_channels, 1)
        self.conv = nn.Conv1d(attn_channels, channels, 1)

    def forward(self, x):
        # x: [B, C, T]
        if self.global_context:
            mean = x.mean(dim=2, keepdim=True).expand_as(x)
            std = x.std(dim=2, keepdim=True).expand_as(x)
            attn_in = torch.cat([x, mean, std], dim=1)
        else:
            attn_in = x
        attn = self.tdnn(attn_in)
        attn = self.conv(attn)
        attn = F.softmax(attn, dim=2)
        mean = (x * attn).sum(dim=2)
        std = torch.sqrt(((x ** 2) * attn).sum(dim=2) - mean ** 2 + 1e-12)
        return torch.cat([mean, std], dim=1)  # [B, C*2]


class ECAPA_TDNN(nn.Module):
    def __init__(self, n_mels=80, channels=1024, emb_dim=192, res2_scale=8):
        super().__init__()
        self.blocks = nn.ModuleList([
            TDNNBlock(n_mels, channels, 5, 1),
            SERes2Block(channels, channels, res2_scale, 3, 2),
            SERes2Block(channels, channels, res2_scale, 3, 3),
            SERes2Block(channels, channels, res2_scale, 3, 4),
        ])
        self.mfa = TDNNBlock(channels * 3, channels * 3, 1)
        self.asp = AttentiveStatisticsPooling(channels * 3, 128)
        self.asp_bn = nn.BatchNorm1d(channels * 6)
        self.fc = nn.Conv1d(channels * 6, emb_dim, 1)

    def forward(self, x, debug=False):
        """x: [B, T, n_mels] -> [B, emb_dim]"""
        # Transpose to [B, n_mels, T] for Conv1d
        x = x.transpose(1, 2)
        intermediates = {}

        # Layer 0
        x = self.blocks[0](x)
        if debug:
            intermediates['layer0'] = x.detach().clone()

        # SE-Res2Blocks
        block_outs = []
        for i in range(1, 4):
            x = self.blocks[i](x)
            block_outs.append(x)
            if debug:
                intermediates[f'block{i}'] = x.detach().clone()

        # MFA
        x = torch.cat(block_outs, dim=1)
        x = self.mfa(x)
        if debug:
            intermediates['mfa'] = x.detach().clone()

        # ASP
        x = self.asp(x)
        if debug:
            intermediates['asp_pool'] = x.detach().clone()

        # ASP BN
        x = self.asp_bn(x)
        if debug:
            intermediates['asp_bn'] = x.detach().clone()

        # FC
        x = x.unsqueeze(2)  # [B, C*6, 1]
        x = self.fc(x).squeeze(2)  # [B, emb_dim]
        if debug:
            intermediates['fc'] = x.detach().clone()

        return x, intermediates


def load_speechbrain_ckpt(model, ckpt_path):
    """Load SpeechBrain checkpoint into our model.
    
    SpeechBrain uses double-nested names like:
      blocks.0.conv.conv.weight -> our blocks[0].conv.weight
      blocks.0.norm.norm.weight -> our blocks[0].norm.weight
    """
    sd = torch.load(ckpt_path, map_location="cpu")
    
    # Build mapping from SpeechBrain keys to our keys
    new_sd = {}
    for key, val in sd.items():
        new_key = key
        # SpeechBrain wraps Conv1d in a class with .conv sub-module:
        #   TDNNBlock: .conv.conv.weight -> .conv.weight, .norm.norm.weight -> .norm.weight
        #   SE block:  .se_block.conv1.conv.weight -> .se_block.conv1.weight
        #   ASP conv:  asp.conv.conv.weight -> asp.conv.weight
        #   FC:        fc.conv.weight -> fc.weight
        #   asp_bn:    asp_bn.norm.weight -> asp_bn.weight
        
        # Strategy: repeatedly remove the double-nesting pattern
        # .conv.conv. -> .conv.  (TDNNBlock conv)
        new_key = new_key.replace(".conv.conv.", ".conv.")
        # .norm.norm. -> .norm.  (TDNNBlock batchnorm)
        new_key = new_key.replace(".norm.norm.", ".norm.")
        # SE block: .conv1.conv. -> .conv1.  and .conv2.conv. -> .conv2.
        new_key = new_key.replace(".conv1.conv.", ".conv1.")
        new_key = new_key.replace(".conv2.conv.", ".conv2.")
        # FC: fc.conv. -> fc.
        if new_key.startswith("fc.conv."):
            new_key = "fc." + new_key[len("fc.conv."):]
        # asp.conv: asp.conv.conv. already handled by first rule
        # asp_bn: asp_bn.norm. -> asp_bn.
        new_key = new_key.replace("asp_bn.norm.", "asp_bn.")
        new_sd[new_key] = val
    
    # Load with strict=False to see what's missing
    missing, unexpected = model.load_state_dict(new_sd, strict=False)
    if missing:
        print(f"WARNING: Missing keys: {missing}")
    if unexpected:
        print(f"WARNING: Unexpected keys: {unexpected}")
    
    return model


def _compute_fbank(audio, sr=16000, n_mels=80):
    """Compute fbank features matching the C++ AudioProcessor.
    
    The C++ code uses:
    - frame_size=400 (25ms), frame_step=160 (10ms)
    - Hamming window
    - Pre-emphasis 0.97
    - DC removal per frame
    - Log mel filterbank
    """
    audio = np.asarray(audio, dtype=np.float32)
    frame_size = 400
    frame_step = 160
    n_fft = 512  # next power of 2 >= 400
    
    n_frames = (len(audio) - frame_size) // frame_step + 1
    if n_frames <= 0:
        return None
    
    # Hamming window
    hamming = 0.54 - 0.46 * np.cos(2 * np.pi * np.arange(frame_size) / frame_size)
    
    # Mel filterbank (matching C++ InitMelFilters)
    def mel_scale(hz):
        return 1127.0 * np.log(1.0 + hz / 700.0)
    
    mel_low_freq = 31.748642
    mel_freq_delta = 34.6702385
    fft_bin_width = sr / n_fft
    num_bins = n_fft // 2
    
    mel_filters = np.zeros((n_mels, num_bins), dtype=np.float32)
    for i in range(n_mels):
        left_mel = mel_low_freq + i * mel_freq_delta
        center_mel = mel_low_freq + (i + 1) * mel_freq_delta
        right_mel = mel_low_freq + (i + 2) * mel_freq_delta
        for j in range(num_bins):
            freq_hz = fft_bin_width * j
            mel_num = mel_scale(freq_hz)
            up_slope = (mel_num - left_mel) / (center_mel - left_mel)
            down_slope = (right_mel - mel_num) / (right_mel - center_mel)
            mel_filters[i, j] = max(0.0, min(up_slope, down_slope))
    
    fbank = np.zeros((n_frames, n_mels), dtype=np.float32)
    
    for i in range(n_frames):
        offset = i * frame_step
        frame = np.zeros(n_fft, dtype=np.float64)
        copy_len = min(frame_size, len(audio) - offset)
        frame[:copy_len] = audio[offset:offset+copy_len]
        
        # DC removal
        frame[:frame_size] -= np.mean(frame[:frame_size])
        
        # Pre-emphasis
        for j in range(frame_size - 1, 0, -1):
            frame[j] -= 0.97 * frame[j-1]
        frame[0] -= 0.97 * frame[0]
        
        # Hamming window
        frame[:frame_size] *= hamming
        
        # FFT
        fft_out = np.fft.rfft(frame)
        power_spec = np.abs(fft_out[:num_bins]) ** 2
        
        # Mel filtering + log
        for j in range(n_mels):
            mel_energy = np.dot(power_spec, mel_filters[j])
            fbank[i, j] = np.log(max(mel_energy, 1.19e-7))
    
    return fbank  # [n_frames, n_mels]


def compute_fbank_pytorch(wav_path, n_mels=80, sample_rate=16000):
    """Compute fbank features from a WAV file."""
    try:
        import soundfile as sf
        audio, sr = sf.read(wav_path)
    except ImportError:
        import wave
        with wave.open(wav_path, 'rb') as wf:
            sr = wf.getframerate()
            audio = np.frombuffer(wf.readframes(wf.getnframes()), dtype=np.int16).astype(np.float32) / 32768.0
    
    if len(audio.shape) > 1:
        audio = audio.mean(axis=1)
    if sr != sample_rate:
        ratio = sample_rate / sr
        n_out = int(len(audio) * ratio)
        audio = np.interp(np.linspace(0, len(audio)-1, n_out), np.arange(len(audio)), audio)
    
    return _compute_fbank(audio, sample_rate, n_mels)


def compute_fbank_pytorch_from_array(audio, sr, n_mels=80):
    """Compute fbank features from a numpy array."""
    return _compute_fbank(audio, sr, n_mels)


def main():
    ckpt_path = os.path.join(os.path.dirname(__file__), "..", "..", "ecapa-raw", "embedding_model.ckpt")
    test_dir = os.path.join(os.path.dirname(__file__), "..", "test", "real_speech")
    
    if not os.path.exists(ckpt_path):
        print(f"Checkpoint not found: {ckpt_path}")
        sys.exit(1)
    
    # Find test WAV files
    wav_files = sorted([f for f in os.listdir(test_dir) if f.endswith('.wav')]) if os.path.isdir(test_dir) else []
    if not wav_files:
        print("No test WAV files found. Using a simple sine wave test.")
        wav_files = []
    
    # Build model and load weights
    print("Building ECAPA-TDNN model...")
    model = ECAPA_TDNN(n_mels=80, channels=1024, emb_dim=192, res2_scale=8)
    model = load_speechbrain_ckpt(model, ckpt_path)
    model.eval()
    
    print(f"\nModel loaded. Parameters: {sum(p.numel() for p in model.parameters()):,}")
    
    # Process each WAV file
    embeddings = {}
    
    if not wav_files:
        # Generate synthetic test signals
        sr = 16000
        t = np.linspace(0, 2, sr * 2)
        signals = {
            "sine_200hz": np.sin(2 * np.pi * 200 * t).astype(np.float32),
            "sine_1000hz": np.sin(2 * np.pi * 1000 * t).astype(np.float32),
        }
        for name, sig in signals.items():
            fbank = compute_fbank_pytorch_from_array(sig, sr)
            if fbank is None:
                continue
            fbank_t = torch.from_numpy(fbank).unsqueeze(0)  # [1, T, 80]
            with torch.no_grad():
                emb, intermediates = model(fbank_t, debug=True)
            emb_np = emb.squeeze(0).numpy()
            emb_np = emb_np / (np.linalg.norm(emb_np) + 1e-10)
            embeddings[name] = emb_np
            print(f"\n{name}: emb norm={np.linalg.norm(emb.numpy()):.4f}")
            print(f"  emb[:10] = {emb_np[:10]}")
            for k, v in intermediates.items():
                print(f"  {k}: shape={list(v.shape)}, mean={v.mean():.6f}, std={v.std():.6f}, "
                      f"min={v.min():.6f}, max={v.max():.6f}")
    else:
        for wav_name in wav_files[:6]:
            wav_path = os.path.join(test_dir, wav_name)
            print(f"\nProcessing: {wav_name}")
            
            fbank = compute_fbank_pytorch(wav_path)
            if fbank is None:
                print(f"  Failed to compute fbank")
                continue
            
            print(f"  Fbank: shape={fbank.shape}, mean={fbank.mean():.4f}, std={fbank.std():.4f}")
            
            fbank_t = torch.from_numpy(fbank).unsqueeze(0)  # [1, T, 80]
            
            with torch.no_grad():
                emb, intermediates = model(fbank_t, debug=True)
            
            emb_np = emb.squeeze(0).numpy()
            raw_norm = np.linalg.norm(emb_np)
            emb_np = emb_np / (raw_norm + 1e-10)
            embeddings[wav_name] = emb_np
            
            print(f"  Raw embedding norm: {raw_norm:.4f}")
            print(f"  Embedding[:10]: {emb_np[:10]}")
            
            for k, v in intermediates.items():
                print(f"  {k}: shape={list(v.shape)}, mean={v.mean():.6f}, std={v.std():.6f}")
    
    # Compute cosine similarities
    if len(embeddings) >= 2:
        names = list(embeddings.keys())
        print(f"\n{'='*60}")
        print("Cosine Similarities (PyTorch reference):")
        print(f"{'='*60}")
        for i in range(len(names)):
            for j in range(i+1, len(names)):
                cos = np.dot(embeddings[names[i]], embeddings[names[j]])
                # Determine if same speaker
                spk_i = names[i].rsplit('_', 1)[0] if '_' in names[i] else names[i]
                spk_j = names[j].rsplit('_', 1)[0] if '_' in names[j] else names[j]
                tag = "SAME" if spk_i == spk_j else "DIFF"
                print(f"  {names[i]:30s} vs {names[j]:30s} => {cos:.4f} [{tag}]")
    
    # Also dump the raw checkpoint keys for debugging
    print(f"\n{'='*60}")
    print("Checkpoint key analysis:")
    sd = torch.load(ckpt_path, map_location="cpu")
    # Show first few conv weights to verify shapes
    for key in sorted(sd.keys()):
        if 'conv.weight' in key or 'shortcut' in key:
            print(f"  {key}: {list(sd[key].shape)}")
            if key == "blocks.0.conv.conv.weight":
                w = sd[key].numpy()
                print(f"    PyTorch layout [OC, IC, K]: [{w.shape[0]}, {w.shape[1]}, {w.shape[2]}]")
                print(f"    ggml expects [K, IC, OC]: transpose(2,1,0)")
                wt = np.transpose(w, (2, 1, 0))
                print(f"    After transpose: [{wt.shape[0]}, {wt.shape[1]}, {wt.shape[2]}]")


if __name__ == "__main__":
    main()

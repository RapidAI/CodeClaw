"""Verify moonshine-tiny encoder output with PyTorch reference.
Loads the same WAV, runs through the HF model, prints encoder stats + first decode token.
"""
import numpy as np
import json, sys
from pathlib import Path

model_dir = Path("models/moonshine-tiny")
wav_path = "test/real_speech/en_female_jenny_0.wav"

# Load audio
import wave
with wave.open(wav_path, "rb") as wf:
    assert wf.getsampwidth() == 2 and wf.getnchannels() == 1
    sr = wf.getframerate()
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"Audio: {len(pcm)} samples @ {sr} Hz")

# Try loading with transformers
try:
    from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor
    import torch

    processor = AutoProcessor.from_pretrained(str(model_dir))
    model = AutoModelForSpeechSeq2Seq.from_pretrained(str(model_dir))
    model.eval()

    inputs = processor(pcm, sampling_rate=16000, return_tensors="pt")
    input_features = inputs.input_features  # [1, seq]

    with torch.no_grad():
        # Encoder
        enc_out = model.model.encoder(input_features)
        hidden = enc_out.last_hidden_state  # [1, frames, dim]
        print(f"Encoder output: shape={hidden.shape}")
        print(f"  mean={hidden.mean().item():.6f}, std={hidden.std().item():.6f}")
        print(f"  min={hidden.min().item():.6f}, max={hidden.max().item():.6f}")
        print(f"  first 5 values: {hidden[0, 0, :5].tolist()}")

        # Full generate
        gen_ids = model.generate(**inputs, max_new_tokens=100)
        text = processor.batch_decode(gen_ids, skip_special_tokens=True)[0]
        print(f"\nTranscription: {text}")

except (ImportError, KeyError, Exception) as e:
    print(f"transformers approach failed: {e}")
    print("Falling back to manual weight inspection...")

    from safetensors import safe_open
    with safe_open(str(model_dir / "model.safetensors"), framework="numpy") as f:
        for name in sorted(f.keys()):
            t = f.get_tensor(name)
            if any(k in name for k in ["conv1", "conv2", "conv3", "embed", "layer_norm", "groupnorm"]):
                print(f"{name}: shape={t.shape}, mean={t.mean():.6f}, std={t.std():.6f}, "
                      f"min={t.min():.6f}, max={t.max():.6f}")

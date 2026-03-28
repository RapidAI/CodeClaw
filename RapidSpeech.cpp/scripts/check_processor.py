"""Check what HF processor does to audio."""
from transformers import AutoProcessor
import numpy as np, wave

p = AutoProcessor.from_pretrained("models/moonshine-tiny")
fe = p.feature_extractor
print(f"Feature extractor type: {type(fe).__name__}")
print(f"Config: {fe.to_dict()}")

# Test with raw PCM
with wave.open("test/real_speech/en_female_jenny_0.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

print(f"\nRaw PCM: min={pcm.min():.6f}, max={pcm.max():.6f}, mean={pcm.mean():.6f}, std={pcm.std():.6f}")

inputs = p(pcm, return_tensors="pt", sampling_rate=16000)
iv = inputs["input_values"][0].numpy()
print(f"After processor: min={iv.min():.6f}, max={iv.max():.6f}, mean={iv.mean():.6f}, std={iv.std():.6f}")

# Check if it's just mean/std normalization
diff = iv - pcm
print(f"Diff (processed - raw): min={diff.min():.6f}, max={diff.max():.6f}")
ratio = iv / (pcm + 1e-10)
# Check if it's a simple scaling
print(f"Ratio first 100 non-zero: {ratio[pcm != 0][:5]}")

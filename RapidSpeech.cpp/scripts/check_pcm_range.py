"""Check PCM value range after loading."""
import numpy as np
import wave

wav_path = "test/real_speech/en_female_jenny_0.wav"
with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    raw = np.frombuffer(frames, dtype=np.int16)
    pcm = raw.astype(np.float32) / 32768.0

print(f"int16 range: [{raw.min()}, {raw.max()}]")
print(f"float range: [{pcm.min():.6f}, {pcm.max():.6f}]")
print(f"samples: {len(pcm)}")
print(f"first 10: {pcm[:10]}")

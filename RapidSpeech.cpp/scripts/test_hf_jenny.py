"""Test en_female_jenny_0.wav with HF official model to get ground truth transcription."""
import numpy as np, wave, os, sys
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

wav_path = "test/real_speech/en_female_jenny_0.wav"
with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"Audio: {len(pcm)/16000:.1f}s")

try:
    from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor
    import torch
except ImportError:
    print("Need: pip install transformers torch"); sys.exit(1)

model_dir = "models/moonshine-tiny"
model = AutoModelForSpeechSeq2Seq.from_pretrained(model_dir)
processor = AutoProcessor.from_pretrained(model_dir)
inputs = processor(pcm, return_tensors="pt", sampling_rate=16000)

gen = model.generate(**inputs, max_length=200)
text = processor.decode(gen[0], skip_special_tokens=True)
print(f"HF result: \"{text}\"")
print(f"Tokens: {gen[0].tolist()}")
print(f"First 10 tokens: {gen[0][:10].tolist()}")

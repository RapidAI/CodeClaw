#!/usr/bin/env python3
"""Test base-zh model on maclaw_16k.wav with HF official pipeline."""
import numpy as np, wave, torch, os, sys
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

with wave.open("test/real_speech/maclaw_16k.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"Audio: {len(pcm)/16000:.1f}s")

from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor
model = AutoModelForSpeechSeq2Seq.from_pretrained("models/moonshine-base-zh")
proc = AutoProcessor.from_pretrained("models/moonshine-base-zh")
inputs = proc(pcm, return_tensors="pt", sampling_rate=16000)

with torch.no_grad():
    # Step 0 logits
    enc = model.model.encoder(input_values=inputs.input_values, attention_mask=inputs.get("attention_mask"))
    bos = torch.tensor([[1]])
    dec = model.model.decoder(input_ids=bos, encoder_hidden_states=enc.last_hidden_state, encoder_attention_mask=enc.attention_mask)
    logits = model.proj_out(dec.last_hidden_state)
    top5 = torch.topk(logits[0, 0], 5)
    print(f"HF step0 top5: ids={top5.indices.tolist()} vals={[f'{v:.2f}' for v in top5.values.tolist()]}")

    # Full generate
    gen = model.generate(**inputs, max_length=200)
    text = proc.decode(gen[0], skip_special_tokens=True)
    print(f"HF transcription: \"{text}\"")
    print(f"Tokens ({len(gen[0])}): {gen[0].tolist()[:30]}")

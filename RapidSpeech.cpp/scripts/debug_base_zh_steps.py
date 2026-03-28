#!/usr/bin/env python3
"""Debug base-zh decoder step by step to find where C++ diverges."""
import numpy as np, wave, torch, torch.nn.functional as F, os
from safetensors import safe_open
from pathlib import Path

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

with wave.open("test/real_speech/maclaw_16k.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

# Use HF to get encoder output and step-by-step decoder
from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor
model = AutoModelForSpeechSeq2Seq.from_pretrained("models/moonshine-base-zh")
proc = AutoProcessor.from_pretrained("models/moonshine-base-zh")
inputs = proc(pcm, return_tensors="pt", sampling_rate=16000)

with torch.no_grad():
    enc = model.model.encoder(input_values=inputs.input_values, attention_mask=inputs.get("attention_mask"))
    
    # Step-by-step greedy decode
    tokens = [1]  # BOS
    for step in range(10):
        input_ids = torch.tensor([tokens])
        dec = model.model.decoder(input_ids=input_ids, 
                                   encoder_hidden_states=enc.last_hidden_state,
                                   encoder_attention_mask=enc.attention_mask)
        logits = model.proj_out(dec.last_hidden_state)
        # Get logits for the last position
        step_logits = logits[0, -1]
        top5 = torch.topk(step_logits, 5)
        next_tok = step_logits.argmax().item()
        print(f"Step {step}: input_ids={tokens}, next={next_tok}, top5 ids={top5.indices.tolist()} vals={[f'{v:.2f}' for v in top5.values.tolist()]}")
        if next_tok == 2:
            print("  -> EOS, stopping")
            break
        tokens.append(next_tok)
    
    print(f"\nFinal tokens: {tokens}")
    text = proc.decode(torch.tensor(tokens), skip_special_tokens=True)
    print(f"Text: \"{text}\"")

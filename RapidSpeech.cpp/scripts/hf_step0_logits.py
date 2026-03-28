"""Get HF model's step 0 raw logits for comparison."""
import numpy as np, wave, torch
from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor

with wave.open("test/real_speech/en_female_jenny_0.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

model = AutoModelForSpeechSeq2Seq.from_pretrained("models/moonshine-tiny")
processor = AutoProcessor.from_pretrained("models/moonshine-tiny")
inputs = processor(pcm, return_tensors="pt", sampling_rate=16000)

# Get encoder output
enc_out = model.get_encoder()(**inputs)
print(f"HF enc_out shape: {enc_out.last_hidden_state.shape}")
print(f"HF enc_out[0,0,:5]: {enc_out.last_hidden_state[0,0,:5].tolist()}")

# Check what inputs contains
print(f"\nInputs keys: {list(inputs.keys())}")
for k, v in inputs.items():
    if hasattr(v, 'shape'):
        print(f"  {k}: shape={v.shape}, dtype={v.dtype}")
        if v.numel() < 20:
            print(f"    values: {v.tolist()}")
        else:
            print(f"    first 5: {v.flatten()[:5].tolist()}")
            print(f"    min={v.min().item():.6f}, max={v.max().item():.6f}")

# Run decoder step 0 with BOS using the correct input key
dec_input = torch.tensor([[1]])  # BOS
with torch.no_grad():
    # Use encoder_outputs directly
    out = model(encoder_outputs=enc_out, decoder_input_ids=dec_input)
logits = out.logits[0, 0]  # [vocab_size]
top5 = torch.topk(logits, 5)
print(f"\nHF step0 logits top5: ids={top5.indices.tolist()}, vals={top5.values.tolist()}")

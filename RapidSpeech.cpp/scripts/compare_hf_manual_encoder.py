"""Compare HF encoder vs manual encoder step by step."""
import numpy as np, wave, torch
import torch.nn.functional as F
from transformers import AutoModelForSpeechSeq2Seq
from safetensors import safe_open

with wave.open("test/real_speech/en_female_jenny_0.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
pcm_t = torch.from_numpy(pcm).unsqueeze(0)

model = AutoModelForSpeechSeq2Seq.from_pretrained("models/moonshine-tiny")
encoder = model.get_encoder()

# Hook into encoder to get intermediate values
intermediates = {}
def hook_fn(name):
    def fn(module, input, output):
        if isinstance(output, tuple):
            intermediates[name] = output[0].detach()
        else:
            intermediates[name] = output.detach()
    return fn

# Register hooks on encoder submodules
for name, module in encoder.named_modules():
    if name:
        module.register_forward_hook(hook_fn(name))

# Run HF encoder
with torch.no_grad():
    hf_enc = encoder(input_values=pcm_t, attention_mask=torch.ones(1, len(pcm), dtype=torch.int32))

print("HF encoder intermediates:")
for name in sorted(intermediates.keys()):
    t = intermediates[name]
    if hasattr(t, 'shape'):
        s = f"shape={list(t.shape)}"
        if t.numel() > 0 and t.numel() < 1000000:
            s += f" mean={t.mean().item():.6f}"
        print(f"  {name}: {s}")

# Now run manual encoder
weights = {}
with safe_open("models/moonshine-tiny/model.safetensors", framework="pt", device="cpu") as f:
    for n in f.keys():
        weights[n] = f.get_tensor(n).float()

x = F.conv1d(pcm_t.unsqueeze(1), weights["model.encoder.conv1.weight"], stride=64)
print(f"\nManual conv1 output: shape={list(x.shape)}, mean={x.mean().item():.6f}")

# Check HF conv1 output
if "conv1" in intermediates:
    hf_conv1 = intermediates["conv1"]
    print(f"HF conv1 output: shape={list(hf_conv1.shape)}, mean={hf_conv1.mean().item():.6f}")
    print(f"Diff: {(x - hf_conv1).abs().max().item():.6f}")

# Check what HF encoder architecture looks like
print(f"\nHF encoder type: {type(encoder).__name__}")
print(f"HF encoder children:")
for name, child in encoder.named_children():
    print(f"  {name}: {type(child).__name__}")

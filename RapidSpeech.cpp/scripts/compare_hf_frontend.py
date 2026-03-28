"""Compare HF encoder frontend vs manual step by step."""
import torch, numpy as np, wave
from transformers import AutoModelForSpeechSeq2Seq
import torch.nn.functional as F
from safetensors import safe_open

with wave.open("test/real_speech/en_female_jenny_0.wav", "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
pcm_t = torch.from_numpy(pcm).unsqueeze(0)

m = AutoModelForSpeechSeq2Seq.from_pretrained("models/moonshine-tiny")
enc = m.get_encoder()

outs = {}
def make_hook(name):
    def hook(mod, inp, out):
        outs[name] = out.detach() if isinstance(out, torch.Tensor) else out[0].detach()
    return hook

enc.conv1.register_forward_hook(make_hook("conv1"))
enc.groupnorm.register_forward_hook(make_hook("gn"))
enc.conv2.register_forward_hook(make_hook("conv2"))
enc.conv3.register_forward_hook(make_hook("conv3"))

with torch.no_grad():
    hf_out = enc(input_values=pcm_t, attention_mask=torch.ones(1, len(pcm), dtype=torch.int32))

# Check what happens between conv1 and groupnorm in HF
# Look at the HF encoder forward method
import inspect
src = inspect.getsource(type(enc).forward)
# Find the relevant lines
for line in src.split("\n"):
    line = line.strip()
    if any(k in line for k in ["conv1", "conv2", "conv3", "tanh", "gelu", "group", "permute", "transpose"]):
        print(f"  HF: {line}")

print()

# Manual
weights = {}
with safe_open("models/moonshine-tiny/model.safetensors", framework="pt", device="cpu") as f:
    for n in f.keys():
        weights[n] = f.get_tensor(n).float()

x = F.conv1d(pcm_t.unsqueeze(1), weights["model.encoder.conv1.weight"], stride=64)
print(f"Conv1 match: {torch.allclose(x, outs['conv1'], atol=1e-5)}")

x_tanh = torch.tanh(x)
x_gn = F.group_norm(x_tanh, 1, weights["model.encoder.groupnorm.weight"], weights["model.encoder.groupnorm.bias"])
print(f"After GN manual: {x_gn[0,:3,500].tolist()}")
print(f"After GN HF:     {outs['gn'][0,:3,500].tolist()}")
print(f"GN match: {torch.allclose(x_gn, outs['gn'], atol=1e-5)}")

# Check if HF applies tanh before or after groupnorm, or in different order
# Print HF conv1 raw output vs after tanh
print(f"\nHF conv1 raw [0,:3,500]: {outs['conv1'][0,:3,500].tolist()}")
print(f"HF conv1 min/max: {outs['conv1'].min().item():.3f} / {outs['conv1'].max().item():.3f}")

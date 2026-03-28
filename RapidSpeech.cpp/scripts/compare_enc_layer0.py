"""Compare HF vs manual encoder after layer 0."""
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
        if isinstance(out, tuple):
            outs[name] = out[0].detach()
        else:
            outs[name] = out.detach()
    return hook

for i in range(6):
    enc.layers[i].register_forward_hook(make_hook(f"layer{i}"))
    enc.layers[i].self_attn.register_forward_hook(make_hook(f"layer{i}_attn"))
    enc.layers[i].mlp.register_forward_hook(make_hook(f"layer{i}_mlp"))

with torch.no_grad():
    hf_out = enc(input_values=pcm_t, attention_mask=torch.ones(1, len(pcm), dtype=torch.int32))

# Manual encoder
weights = {}
with safe_open("models/moonshine-tiny/model.safetensors", framework="pt", device="cpu") as f:
    for n in f.keys():
        weights[n] = f.get_tensor(n).float()

dim = 288; n_heads = 8; head_dim = 36; rotary_dim = 32
scale = 1.0 / (head_dim ** 0.5)

def layer_norm(x, w, eps=1e-5):
    return F.layer_norm(x, (dim,), w, None, eps)

def apply_rotary(x, rotary_dim=32, theta=10000.0):
    seq_len = x.shape[2]
    pos = torch.arange(seq_len, dtype=torch.float32)
    dim_pairs = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
    cos_a = torch.cos(angles).unsqueeze(0).unsqueeze(0)
    sin_a = torch.sin(angles).unsqueeze(0).unsqueeze(0)
    x_rot = x[..., :rotary_dim]; x_pass = x[..., rotary_dim:]
    x1 = x_rot[..., :dim_pairs]; x2 = x_rot[..., dim_pairs:]
    return torch.cat([x1*cos_a - x2*sin_a, x2*cos_a + x1*sin_a, x_pass], dim=-1)

# Frontend
x = F.conv1d(pcm_t.unsqueeze(1), weights["model.encoder.conv1.weight"], stride=64)
x = torch.tanh(x)
x = F.group_norm(x, 1, weights["model.encoder.groupnorm.weight"], weights["model.encoder.groupnorm.bias"])
x = F.gelu(F.conv1d(x, weights["model.encoder.conv2.weight"], weights["model.encoder.conv2.bias"], stride=3))
x = F.gelu(F.conv1d(x, weights["model.encoder.conv3.weight"], weights["model.encoder.conv3.bias"], stride=2))
x = x.permute(0, 2, 1)  # [1, 182, 288]

# Layer 0
i = 0
residual = x
x_norm = layer_norm(x, weights[f"model.encoder.layers.{i}.input_layernorm.weight"])
q = F.linear(x_norm, weights[f"model.encoder.layers.{i}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
k = F.linear(x_norm, weights[f"model.encoder.layers.{i}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
v = F.linear(x_norm, weights[f"model.encoder.layers.{i}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
q = apply_rotary(q); k = apply_rotary(k)
attn = F.softmax(torch.matmul(q, k.transpose(-2,-1)) * scale, dim=-1)
out = torch.matmul(attn, v).permute(0,2,1,3).reshape(1,-1,dim)
attn_out = F.linear(out, weights[f"model.encoder.layers.{i}.self_attn.o_proj.weight"])
x_after_attn = residual + attn_out

print(f"Manual after L0 attn [0,0,:5]: {x_after_attn[0,0,:5].tolist()}")
print(f"HF after L0 attn [0,0,:5]:     {outs['layer0_attn'][0,0,:5].tolist()}")

# Note: HF layer0_attn hook captures the attention output (before residual add)
# Let's check
print(f"\nManual attn_out [0,0,:5]: {attn_out[0,0,:5].tolist()}")
print(f"HF attn_out [0,0,:5]:     {outs['layer0_attn'][0,0,:5].tolist()}")
diff = (attn_out - outs['layer0_attn']).abs().max().item()
print(f"Attn output max diff: {diff:.8f}")

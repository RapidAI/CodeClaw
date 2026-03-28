# Moonshine ASR Architecture Notes

Reference for reimplementing moonshine.cpp to match the actual HuggingFace model.

## Source: usefulsensors/moonshine-tiny

### AudioPreprocessor (encoder frontend)
```python
nn.Conv1d(1, dim=288, kernel=127, stride=64, bias=False)  # -> [batch, 288, OL1]
nn.Tanh()                                                   # NOT GELU
nn.GroupNorm(1, 288)                                        # group=1 = LayerNorm over channels
nn.Conv1d(288, 576, kernel=7, stride=3, bias=True)          # -> [batch, 576, OL2]
nn.GELU()
nn.Conv1d(576, 288, kernel=3, stride=2, bias=True)          # -> [batch, 288, OL3]
nn.GELU()
Rearrange("b c s -> b s c")                                 # transpose to [batch, seq, dim]
```
Total downsampling: 64 * 3 * 2 = 384x
32000 samples (2s @ 16kHz) -> 499 -> 165 -> 82 frames

### GGUF Tensor Names (all prefixed with "model.")
```
model.encoder.conv1.weight          [127, 1, 288]    (no bias)
model.encoder.conv2.weight          [7, 288, 576]
model.encoder.conv2.bias            [576]
model.encoder.conv3.weight          [3, 576, 288]
model.encoder.conv3.bias            [288]
model.encoder.groupnorm.weight      [288]
model.encoder.groupnorm.bias        [288]
model.encoder.layer_norm.weight     [288]            (final encoder norm)

model.encoder.layers.{i}.input_layernorm.weight              [288]  (LayerNorm, no bias)
model.encoder.layers.{i}.self_attn.q_proj.weight             [288, 288]
model.encoder.layers.{i}.self_attn.k_proj.weight             [288, 288]
model.encoder.layers.{i}.self_attn.v_proj.weight             [288, 288]
model.encoder.layers.{i}.self_attn.o_proj.weight             [288, 288]
model.encoder.layers.{i}.post_attention_layernorm.weight     [288]  (LayerNorm)
model.encoder.layers.{i}.mlp.fc1.weight                      [288, 1152]
model.encoder.layers.{i}.mlp.fc1.bias                        [1152]
model.encoder.layers.{i}.mlp.fc2.weight                      [1152, 288]
model.encoder.layers.{i}.mlp.fc2.bias                        [288]

model.decoder.embed_tokens.weight                            [288, 32768]
model.decoder.layers.{i}.input_layernorm.weight              [288]
model.decoder.layers.{i}.self_attn.q_proj.weight             [288, 288]
model.decoder.layers.{i}.self_attn.k_proj.weight             [288, 288]
model.decoder.layers.{i}.self_attn.v_proj.weight             [288, 288]
model.decoder.layers.{i}.self_attn.o_proj.weight             [288, 288]
model.decoder.layers.{i}.post_attention_layernorm.weight     [288]
model.decoder.layers.{i}.encoder_attn.q_proj.weight          [288, 288]
model.decoder.layers.{i}.encoder_attn.k_proj.weight          [288, 288]
model.decoder.layers.{i}.encoder_attn.v_proj.weight          [288, 288]
model.decoder.layers.{i}.encoder_attn.o_proj.weight          [288, 288]
model.decoder.layers.{i}.final_layernorm.weight              [288]
model.decoder.layers.{i}.mlp.fc1.weight                      [288, 2304]  (SwiGLU: 2*4*dim)
model.decoder.layers.{i}.mlp.fc1.bias                        [2304]
model.decoder.layers.{i}.mlp.fc2.weight                      [1152, 288]
model.decoder.layers.{i}.mlp.fc2.bias                        [288]
```

### Key Architecture Details
- Encoder: LayerNorm (no bias), separate Q/K/V, GELU FFN, no attention bias
- Decoder: LayerNorm (no bias), separate Q/K/V, SiLU/SwiGLU FFN, causal self-attn
- Decoder uses SwiGLU: fc1 output is 2*intermediate, split into gate+value
- RoPE: partial_rotary_factor=0.9, theta=10000, interleaved mode (NOT split-half)
  - HF rotate_half: x1=x[...,0::2], x2=x[...,1::2] → interleaved pairs
  - ggml: mode=0 (default), NOT mode=2 (NeoX split-half)
- No attention bias anywhere
- Vocab: 32768, BOS=1, EOS=2
- Tiny: dim=288, enc_depth=6, dec_depth=6, heads=8, head_dim=36
- No lm_head weight — uses weight tying (embed_tokens = lm_head)
- PCM input must be normalized to [-1, 1] (int16 / 32768.0)
- HF processor (Wav2Vec2FeatureExtractor) does NOT normalize (do_normalize=False)

### ggml conv_1d Notes
- `ggml_conv_1d(ctx, kernel, input, stride, padding, dilation)`
- kernel: [kernel_size, in_channels, out_channels]
- input: [length, in_channels]
- output: 3D [OL, OC, N], needs reshape_2d to [OL, OC]
- Bias add: bias [OC] needs reshape to [1, OC] for broadcasting with [OL, OC]

### Bugs Fixed (verified against HF transformers)
1. GroupNorm(1, dim): was using transpose→ggml_norm→transpose (per-timestep norm), fixed to ggml_group_norm(n_groups=1) (whole-plane norm)
2. PCM normalization: load_wav_file was not dividing int16 by 32768.0
3. Norm type: was using RMSNorm, fixed to LayerNorm (matching HF's nn.LayerNorm)
4. RoPE mode: was using mode=2 (NeoX split-half), fixed to mode=0 (interleaved), matching HF's rotate_half
5. SwiGLU gate/value order: HF does `hidden_states, gate = fc1.chunk(2)` then `silu(gate) * hidden_states`.
   So first half of fc1 output = value (no activation), second half = gate (through silu).
   Was incorrectly reversed (first half = gate). Fixed in both C++ and Python reference scripts.
6. Python reference scripts RoPE: was using split-half (x[:dp], x[dp:]) instead of interleaved (x[0::2], x[1::2]).
   Fixed to match HF's rotate_half. C++ was already correct (ggml mode=0).

### Current Status
- Encoder output matches HF to ~1e-3 precision
- Bug 5 (SwiGLU gate/value order) fixed — was the root cause of decoder logit mismatch
- Need to rebuild and verify decoder step 0 logits match HF (expected top1=450)
- If still mismatched after SwiGLU fix, check: decoder norm type consistency, cross-attn RoPE (should NOT apply RoPE to cross-attn Q)

### HF Official Transformers Implementation Reference
- Source: github.com/huggingface/transformers/blob/main/src/transformers/models/moonshine/modeling_moonshine.py
- MoonshineDecoderMLP.forward:
  ```python
  hidden_states = self.fc1(hidden_states)
  hidden_states, gate = hidden_states.chunk(2, dim=-1)
  hidden_states = self.activation_fn(gate) * hidden_states  # silu(second_half) * first_half
  hidden_states = self.fc2(hidden_states)
  ```
- rotate_half is interleaved: x1 = x[..., 0::2], x2 = x[..., 1::2] → ggml mode=0
- All norms are nn.LayerNorm(bias=False)
- Cross-attention does NOT apply RoPE (only self-attention does)
- Weight tying: proj_out.weight = model.decoder.embed_tokens.weight

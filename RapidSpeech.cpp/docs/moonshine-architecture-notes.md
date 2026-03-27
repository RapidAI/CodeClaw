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

model.encoder.layers.{i}.input_layernorm.weight              [288]  (RMSNorm, no bias)
model.encoder.layers.{i}.self_attn.q_proj.weight             [288, 288]
model.encoder.layers.{i}.self_attn.k_proj.weight             [288, 288]
model.encoder.layers.{i}.self_attn.v_proj.weight             [288, 288]
model.encoder.layers.{i}.self_attn.o_proj.weight             [288, 288]
model.encoder.layers.{i}.post_attention_layernorm.weight     [288]  (RMSNorm)
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
- Encoder: RMSNorm (no bias), separate Q/K/V, GELU FFN, no attention bias
- Decoder: RMSNorm, separate Q/K/V, SiLU/SwiGLU FFN, causal self-attn
- Decoder uses SwiGLU: fc1 output is 2*intermediate, split into gate+value
- RoPE: partial_rotary_factor=0.9, theta=10000
- No attention bias anywhere
- Vocab: 32768, BOS=1, EOS=2
- Tiny: dim=288, enc_depth=6, dec_depth=6, heads=8, head_dim=36

### ggml conv_1d Notes
- `ggml_conv_1d(ctx, kernel, input, stride, padding, dilation)`
- kernel: [kernel_size, in_channels, out_channels]
- input: [length, in_channels]
- output: 3D [OL, OC, N], needs reshape_2d to [OL, OC]
- Bias add: bias [OC] needs reshape to [1, OC] for broadcasting with [OL, OC]

### Known Issues from Previous Attempt
- GroupNorm crash: `layer_norm(x[288,499], weight[288])` crashed silently
  - Possibly ggml_norm on transposed tensor issue
  - Try: use ggml_group_norm if available, or apply norm on [OL, dim] directly
- Debug log patches made the file unmaintainable -> full rewrite needed

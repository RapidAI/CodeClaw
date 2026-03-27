#include "arch/moonshine.h"
#include "core/rs_context.h"
#include "utils/rs_log.h"
#include "ggml.h"
#include "ggml-backend.h"
#include "ggml-alloc.h"
#include "gguf.h"

#include <cmath>
#include <cstring>
#include <algorithm>
#include <sstream>

// ============================================================
// Moonshine ASR — ggml native encoder-decoder transformer
//
// Architecture (arxiv 2410.15608, usefulsensors/moonshine-tiny):
//   Audio -> Conv frontend -> Encoder (RMSNorm + RoPE + GELU FFN)
//         -> Decoder (RMSNorm + RoPE + SwiGLU FFN + cross-attn)
//         -> Token logits
//
// Encoder: RMSNorm, separate Q/K/V, GELU FFN, no attention bias
// Decoder: RMSNorm, separate Q/K/V, SwiGLU FFN, causal self-attn
// RoPE: partial_rotary_factor=0.9, theta=10000
// ============================================================

// ---- Norm helpers ----

static ggml_tensor* rms_norm(ggml_context* ctx, ggml_tensor* x,
                              ggml_tensor* weight) {
    x = ggml_rms_norm(ctx, x, 1e-5f);
    if (weight) x = ggml_mul(ctx, x, weight);
    return x;
}

// GroupNorm(1, dim) = LayerNorm over the channel dimension.
// Input x: [time, channels] from conv1d output.
// ggml_norm normalizes over dim0, so we transpose to [channels, time],
// apply norm (normalizes each time-step's channel vector), then transpose back.
static ggml_tensor* group_norm_1(ggml_context* ctx, ggml_tensor* x,
                                  ggml_tensor* weight, ggml_tensor* bias) {
    ggml_tensor* xt = ggml_cont(ctx, ggml_transpose(ctx, x));
    xt = ggml_norm(ctx, xt, 1e-5f);
    if (weight) xt = ggml_mul(ctx, xt, weight);
    if (bias) xt = ggml_add(ctx, xt, bias);
    return ggml_cont(ctx, ggml_transpose(ctx, xt));
}

// ---- RoPE helper ----

static ggml_tensor* apply_rope(ggml_context* ctx, ggml_tensor* x,
                                ggml_tensor* pos, int head_dim,
                                float rope_theta) {
    int rotary_dim = (int)(head_dim * 0.9f);
    rotary_dim -= rotary_dim % 2;  // must be even
    return ggml_rope_ext(ctx, x, pos, nullptr, rotary_dim, 0, 0,
                         rope_theta, 1.0f, 0.0f, 1.0f, 0.0f, 0.0f);
}

// ---- Scaled dot-product attention (F32) ----
// q: [head_dim, n_heads, seq_q]
// k: [head_dim, n_heads, seq_k]
// v: [head_dim, n_heads, seq_k]
// Returns: [dim, seq_q] where dim = head_dim * n_heads

static ggml_tensor* sdpa_attention(ggml_context* ctx,
                                    ggml_tensor* q, ggml_tensor* k,
                                    ggml_tensor* v,
                                    int head_dim, int n_heads,
                                    int seq_q, int seq_k) {
    float scale = 1.0f / sqrtf((float)head_dim);
    q = ggml_scale(ctx, q, scale);

    // Permute all to [head_dim, seq, n_heads] (swap dim1 and dim2)
    ggml_tensor* qp = ggml_cont(ctx, ggml_permute(ctx, q, 0, 2, 1, 3));
    ggml_tensor* kp = ggml_cont(ctx, ggml_permute(ctx, k, 0, 2, 1, 3));
    ggml_tensor* vp = ggml_cont(ctx, ggml_permute(ctx, v, 0, 2, 1, 3));

    // scores = mul_mat(kp, qp) -> [seq_k, seq_q, n_heads]
    ggml_tensor* scores = ggml_mul_mat(ctx, kp, qp);
    scores = ggml_soft_max(ctx, scores);

    // vp_t = permute(vp, 1,0,2,3) -> [seq_k, head_dim, n_heads]
    ggml_tensor* vp_t = ggml_cont(ctx, ggml_permute(ctx, vp, 1, 0, 2, 3));

    // attn_out = mul_mat(vp_t, scores) -> [head_dim, seq_q, n_heads]
    ggml_tensor* attn_out = ggml_mul_mat(ctx, vp_t, scores);

    // Permute back to [head_dim, n_heads, seq_q], reshape to [dim, seq_q]
    attn_out = ggml_cont(ctx, ggml_permute(ctx, attn_out, 0, 2, 1, 3));
    int dim = head_dim * n_heads;
    attn_out = ggml_reshape_2d(ctx, attn_out, dim, seq_q);
    return attn_out;
}

// ============================================================
// Constructor / Destructor
// ============================================================

MoonshineModel::MoonshineModel() {
    meta_.arch_name = "moonshine";
    meta_.audio_sample_rate = 16000;
    meta_.n_mels = 0;
    meta_.vocab_size = 32768;
}

MoonshineModel::~MoonshineModel() = default;

// ============================================================
// Weight loading
// ============================================================

bool MoonshineModel::MapTensors(ggml_context* gguf_data) {
    auto get = [&](const char* name) -> ggml_tensor* {
        ggml_tensor* t = ggml_get_tensor(gguf_data, name);
        if (t) return t;
        std::string prefixed = std::string("model.") + name;
        return ggml_get_tensor(gguf_data, prefixed.c_str());
    };

    // Frontend convolutions
    weights_.frontend_conv1_weight = get("encoder.conv1.weight");
    weights_.frontend_conv1_bias   = get("encoder.conv1.bias");
    weights_.frontend_conv2_weight = get("encoder.conv2.weight");
    weights_.frontend_conv2_bias   = get("encoder.conv2.bias");
    weights_.frontend_conv3_weight = get("encoder.conv3.weight");
    weights_.frontend_conv3_bias   = get("encoder.conv3.bias");
    weights_.frontend_groupnorm_weight = get("encoder.groupnorm.weight");
    weights_.frontend_groupnorm_bias   = get("encoder.groupnorm.bias");

    // Encoder layers — separate Q/K/V, RMSNorm, GELU FFN
    weights_.encoder_layers.resize(hparams_.encoder_depth);
    for (int i = 0; i < hparams_.encoder_depth; i++) {
        auto& layer = weights_.encoder_layers[i];
        char buf[256];
        auto gn = [&](const char* suffix) -> ggml_tensor* {
            snprintf(buf, sizeof(buf), "encoder.layers.%d.%s", i, suffix);
            return get(buf);
        };

        layer.attn_q_weight    = gn("self_attn.q_proj.weight");
        layer.attn_k_weight    = gn("self_attn.k_proj.weight");
        layer.attn_v_weight    = gn("self_attn.v_proj.weight");
        layer.attn_qkv_weight  = nullptr;
        layer.attn_qkv_bias    = nullptr;
        layer.attn_out_weight  = gn("self_attn.o_proj.weight");
        layer.attn_out_bias    = nullptr;
        layer.attn_norm_weight = gn("input_layernorm.weight");
        layer.attn_norm_bias   = nullptr;

        layer.ff_up_weight   = gn("mlp.fc1.weight");
        layer.ff_up_bias     = gn("mlp.fc1.bias");
        layer.ff_down_weight = gn("mlp.fc2.weight");
        layer.ff_down_bias   = gn("mlp.fc2.bias");
        layer.ff_norm_weight = gn("post_attention_layernorm.weight");
        layer.ff_norm_bias   = nullptr;
    }
    weights_.encoder_final_norm_weight = get("encoder.layer_norm.weight");
    weights_.encoder_final_norm_bias   = nullptr;

    // Decoder layers — separate Q/K/V, RMSNorm, SwiGLU FFN
    weights_.decoder_layers.resize(hparams_.decoder_depth);
    for (int i = 0; i < hparams_.decoder_depth; i++) {
        auto& layer = weights_.decoder_layers[i];
        char buf[256];
        auto gn = [&](const char* suffix) -> ggml_tensor* {
            snprintf(buf, sizeof(buf), "decoder.layers.%d.%s", i, suffix);
            return get(buf);
        };

        layer.self_attn_q_weight   = gn("self_attn.q_proj.weight");
        layer.self_attn_k_weight   = gn("self_attn.k_proj.weight");
        layer.self_attn_v_weight   = gn("self_attn.v_proj.weight");
        layer.self_attn_out_weight = gn("self_attn.o_proj.weight");
        layer.self_attn_norm_weight = gn("input_layernorm.weight");
        layer.self_attn_norm_bias   = nullptr;

        layer.cross_attn_q_weight   = gn("encoder_attn.q_proj.weight");
        layer.cross_attn_k_weight   = gn("encoder_attn.k_proj.weight");
        layer.cross_attn_v_weight   = gn("encoder_attn.v_proj.weight");
        layer.cross_attn_out_weight = gn("encoder_attn.o_proj.weight");
        layer.cross_attn_norm_weight = gn("post_attention_layernorm.weight");
        layer.cross_attn_norm_bias   = nullptr;

        layer.ff_up_weight   = gn("mlp.fc1.weight");
        layer.ff_up_bias     = gn("mlp.fc1.bias");
        layer.ff_down_weight = gn("mlp.fc2.weight");
        layer.ff_down_bias   = gn("mlp.fc2.bias");
        layer.ff_norm_weight = gn("final_layernorm.weight");
        layer.ff_norm_bias   = nullptr;
    }
    weights_.decoder_final_norm_weight = get("decoder.layer_norm.weight");
    weights_.decoder_final_norm_bias   = nullptr;

    weights_.token_embedding = get("decoder.embed_tokens.weight");
    weights_.lm_head_weight  = get("lm_head.weight");
    weights_.lm_head_bias    = get("lm_head.bias");

    if (!weights_.frontend_conv1_weight) {
        RS_LOG_ERR("Moonshine: frontend conv1 weight missing");
        return false;
    }
    if (!weights_.token_embedding) {
        RS_LOG_ERR("Moonshine: token embedding missing");
        return false;
    }
    return true;
}

bool MoonshineModel::LoadVocab(gguf_context* ctx_gguf) {
    int64_t key = gguf_find_key(ctx_gguf, "tokenizer.ggml.tokens");
    if (key < 0) key = gguf_find_key(ctx_gguf, "tokenizer.tokens");
    if (key < 0) {
        RS_LOG_WARN("Moonshine: no tokenizer tokens in GGUF");
        return true;
    }
    int n_tokens = gguf_get_arr_n(ctx_gguf, key);
    for (int i = 0; i < n_tokens; i++) {
        const char* tok = gguf_get_arr_str(ctx_gguf, key, i);
        if (tok) vocab_[i] = std::string(tok);
    }
    RS_LOG_INFO("Moonshine: loaded %d vocab tokens", n_tokens);
    return true;
}

// ============================================================
// Load: read GGUF metadata + map tensors + load vocab
// ============================================================

bool MoonshineModel::Load(const std::unique_ptr<rs_context_t>& ctx,
                          ggml_backend_t /*backend*/) {
    if (!ctx || !ctx->gguf_data || !ctx->ctx_gguf) return false;

    auto read_i32 = [&](const char* key, int def) -> int {
        int64_t k = gguf_find_key(ctx->ctx_gguf, key);
        return (k >= 0) ? gguf_get_val_i32(ctx->ctx_gguf, k) : def;
    };
    auto read_f32 = [&](const char* key, float def) -> float {
        int64_t k = gguf_find_key(ctx->ctx_gguf, key);
        return (k >= 0) ? gguf_get_val_f32(ctx->ctx_gguf, k) : def;
    };
    auto read_bool = [&](const char* key, bool def) -> bool {
        int64_t k = gguf_find_key(ctx->ctx_gguf, key);
        return (k >= 0) ? gguf_get_val_bool(ctx->ctx_gguf, k) : def;
    };

    hparams_.encoder_dim   = read_i32("moonshine.encoder_dim", 288);
    hparams_.encoder_depth = read_i32("moonshine.encoder_depth", 6);
    hparams_.encoder_heads = read_i32("moonshine.encoder_heads", 8);
    hparams_.decoder_dim   = read_i32("moonshine.decoder_dim", 288);
    hparams_.decoder_depth = read_i32("moonshine.decoder_depth", 6);
    hparams_.decoder_heads = read_i32("moonshine.decoder_heads", 8);
    hparams_.vocab_size    = read_i32("moonshine.vocab_size", 32768);
    hparams_.max_seq_len   = read_i32("moonshine.max_seq_len", 448);
    hparams_.bos_id        = read_i32("moonshine.bos_id", 1);
    hparams_.eos_id        = read_i32("moonshine.eos_id", 2);
    hparams_.sample_rate   = read_i32("moonshine.sample_rate", 16000);
    hparams_.is_streaming  = read_bool("moonshine.is_streaming", false);
    hparams_.rope_theta    = read_f32("moonshine.rope_theta", 10000.0f);
    hparams_.encoder_head_dim = hparams_.encoder_dim / hparams_.encoder_heads;
    hparams_.decoder_head_dim = hparams_.decoder_dim / hparams_.decoder_heads;
    meta_.audio_sample_rate = hparams_.sample_rate;
    meta_.vocab_size = hparams_.vocab_size;

    if (!MapTensors(ctx->gguf_data)) return false;
    if (!LoadVocab(ctx->ctx_gguf)) return false;

    RS_LOG_INFO("Moonshine: enc=%dx%d dec=%dx%d vocab=%d streaming=%d",
                hparams_.encoder_dim, hparams_.encoder_depth,
                hparams_.decoder_dim, hparams_.decoder_depth,
                hparams_.vocab_size, hparams_.is_streaming);
    return true;
}

std::shared_ptr<RSState> MoonshineModel::CreateState() {
    return std::make_shared<MoonshineState>();
}

// ============================================================
// RoPE helper (member, for encoder full-sequence mode)
// ============================================================

ggml_tensor* MoonshineModel::ApplyRoPE(ggml_context* ctx0, ggml_tensor* x,
                                        int head_dim, int /*offset*/) {
    return apply_rope(ctx0, x, nullptr, head_dim, hparams_.rope_theta);
}

// ============================================================
// Encoder: Conv frontend + transformer layers
// ============================================================

ggml_tensor* MoonshineModel::BuildEncoder(ggml_context* ctx0,
                                           ggml_tensor* audio_input,
                                           int /*n_samples*/, bool /*causal*/,
                                           ggml_tensor** out_positions) {
    const int dim = hparams_.encoder_dim;
    const int n_heads = hparams_.encoder_heads;
    const int head_dim = hparams_.encoder_head_dim;

    if (!weights_.frontend_conv1_weight || !weights_.frontend_conv2_weight ||
        !weights_.frontend_conv3_weight) {
        RS_LOG_ERR("Moonshine: frontend conv weights missing");
        return nullptr;
    }

    // --- AudioPreprocessor ---
    // Conv1d(1, dim, 127, stride=64, bias=False) -> Tanh -> GroupNorm(1, dim)
    // Conv1d(dim, 2*dim, 7, stride=3, bias=True) -> GELU
    // Conv1d(2*dim, dim, 3, stride=2, bias=True) -> GELU
    // -> transpose to [dim, n_frames]

    // ggml conv1d expects input [length, in_channels]
    ggml_tensor* x = ggml_reshape_2d(ctx0, audio_input,
                                      (int)audio_input->ne[0], 1);

    // Helper: add bias [OC] to conv output [OL, OC]
    auto add_bias = [&](ggml_tensor* out, ggml_tensor* bias) -> ggml_tensor* {
        if (!bias) return out;
        ggml_tensor* b = ggml_reshape_2d(ctx0, bias, 1, (int)bias->ne[0]);
        return ggml_add(ctx0, out, b);
    };

    // conv1: stride=64, no bias, Tanh
    // ggml conv_1d im2col requires kernel in F16
    x = ggml_conv_1d(ctx0, ggml_cast(ctx0, weights_.frontend_conv1_weight, GGML_TYPE_F16),
                     x, 64, 0, 1);
    x = ggml_reshape_2d(ctx0, x, (int)x->ne[0], (int)x->ne[1]);
    x = add_bias(x, weights_.frontend_conv1_bias);
    x = ggml_tanh(ctx0, x);

    // GroupNorm(1, dim): norm over channel dim
    x = group_norm_1(ctx0, x, weights_.frontend_groupnorm_weight,
                     weights_.frontend_groupnorm_bias);

    // conv2: stride=3, GELU
    x = ggml_conv_1d(ctx0, ggml_cast(ctx0, weights_.frontend_conv2_weight, GGML_TYPE_F16),
                     x, 3, 0, 1);
    x = ggml_reshape_2d(ctx0, x, (int)x->ne[0], (int)x->ne[1]);
    x = add_bias(x, weights_.frontend_conv2_bias);
    x = ggml_gelu(ctx0, x);

    // conv3: stride=2, GELU
    x = ggml_conv_1d(ctx0, ggml_cast(ctx0, weights_.frontend_conv3_weight, GGML_TYPE_F16),
                     x, 2, 0, 1);
    x = ggml_reshape_2d(ctx0, x, (int)x->ne[0], (int)x->ne[1]);
    x = add_bias(x, weights_.frontend_conv3_bias);
    x = ggml_gelu(ctx0, x);

    // Transpose to [dim, n_frames] for transformer (ggml matmul convention)
    int n_frames = (int)x->ne[0];
    x = ggml_cont(ctx0, ggml_transpose(ctx0, x));

    // Create position tensor for encoder RoPE: [0, 1, 2, ..., n_frames-1]
    ggml_tensor* enc_positions = ggml_new_tensor_1d(ctx0, GGML_TYPE_I32, n_frames);
    ggml_set_name(enc_positions, "enc_positions");
    ggml_set_input(enc_positions);
    if (out_positions) *out_positions = enc_positions;

    // --- Encoder transformer layers ---
    for (int l = 0; l < hparams_.encoder_depth; l++) {
        auto& layer = weights_.encoder_layers[l];

        // Pre-norm self-attention (RMSNorm)
        ggml_tensor* residual = x;
        x = rms_norm(ctx0, x, layer.attn_norm_weight);

        // Separate Q/K/V projections -> [dim, n_frames] each
        ggml_tensor* q = ggml_mul_mat(ctx0, layer.attn_q_weight, x);
        ggml_tensor* k = ggml_mul_mat(ctx0, layer.attn_k_weight, x);
        ggml_tensor* v = ggml_mul_mat(ctx0, layer.attn_v_weight, x);

        // Reshape to [head_dim, n_heads, n_frames]
        q = ggml_reshape_3d(ctx0, q, head_dim, n_heads, n_frames);
        k = ggml_reshape_3d(ctx0, k, head_dim, n_heads, n_frames);
        v = ggml_reshape_3d(ctx0, v, head_dim, n_heads, n_frames);

        // RoPE
        q = apply_rope(ctx0, q, enc_positions, head_dim, hparams_.rope_theta);
        k = apply_rope(ctx0, k, enc_positions, head_dim, hparams_.rope_theta);

        // Scaled dot-product attention
        ggml_tensor* attn_out = sdpa_attention(ctx0, q, k, v,
                                                head_dim, n_heads,
                                                n_frames, n_frames);

        // Output projection + residual
        attn_out = ggml_mul_mat(ctx0, layer.attn_out_weight, attn_out);
        x = ggml_add(ctx0, residual, attn_out);

        // Pre-norm FFN (RMSNorm + GELU)
        residual = x;
        x = rms_norm(ctx0, x, layer.ff_norm_weight);
        x = ggml_mul_mat(ctx0, layer.ff_up_weight, x);
        if (layer.ff_up_bias) x = ggml_add(ctx0, x, layer.ff_up_bias);
        x = ggml_gelu(ctx0, x);
        x = ggml_mul_mat(ctx0, layer.ff_down_weight, x);
        if (layer.ff_down_bias) x = ggml_add(ctx0, x, layer.ff_down_bias);
        x = ggml_add(ctx0, residual, x);
    }

    // Final encoder norm
    x = rms_norm(ctx0, x, weights_.encoder_final_norm_weight);
    return x;
}

// ============================================================
// Encode: audio PCM -> encoder hidden states
// ============================================================

bool MoonshineModel::Encode(const std::vector<float>& input_frames,
                            RSState& state, ggml_backend_sched_t sched) {
    auto& ms = dynamic_cast<MoonshineState&>(state);
    if (input_frames.empty()) return false;

    int n_samples = (int)input_frames.size();
    const int n_nodes = 65536;
    struct ggml_context* ctx0 = nullptr;
    struct ggml_cgraph* gf = nullptr;
    if (!init_compute_ctx(&ctx0, &gf, n_nodes)) return false;

    ggml_tensor* audio_in = ggml_new_tensor_1d(ctx0, GGML_TYPE_F32, n_samples);
    ggml_set_name(audio_in, "audio_pcm");
    ggml_set_input(audio_in);

    ggml_tensor* enc_positions_out = nullptr;
    ggml_tensor* enc_out = BuildEncoder(ctx0, audio_in, n_samples, false,
                                         &enc_positions_out);
    if (!enc_out) {
        RS_LOG_ERR("Moonshine: BuildEncoder returned null");
        ggml_free(ctx0);
        return false;
    }
    ggml_set_name(enc_out, "encoder_output");
    ggml_set_output(enc_out);
    ggml_build_forward_expand(gf, enc_out);

    if (!ggml_backend_sched_alloc_graph(sched, gf)) {
        RS_LOG_ERR("Moonshine: encoder graph allocation failed");
        ggml_free(ctx0);
        return false;
    }

    ggml_backend_tensor_set(audio_in, input_frames.data(), 0,
                            n_samples * sizeof(float));

    // Fill encoder position tensor [0, 1, 2, ..., n_frames-1]
    if (enc_positions_out) {
        int pos_n = (int)enc_positions_out->ne[0];
        std::vector<int32_t> pos_data(pos_n);
        for (int i = 0; i < pos_n; i++) pos_data[i] = i;
        ggml_backend_tensor_set(enc_positions_out, pos_data.data(), 0,
                                pos_n * sizeof(int32_t));
    }

    if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
        RS_LOG_ERR("Moonshine: encoder graph compute failed");
        ggml_free(ctx0);
        return false;
    }

    int enc_dim = hparams_.encoder_dim;
    int enc_frames = (int)(ggml_nelements(enc_out) / enc_dim);
    ms.encoder_out.resize(enc_dim * enc_frames);
    ggml_backend_tensor_get(enc_out, ms.encoder_out.data(), 0,
                            ms.encoder_out.size() * sizeof(float));
    ms.encoder_frames = enc_frames;

    ggml_free(ctx0);
    RS_LOG_INFO("Moonshine: encoded %d samples -> %d frames", n_samples, enc_frames);
    return true;
}

// ============================================================
// Decoder KV cache allocation
// ============================================================

bool MoonshineModel::AllocDecoderCache(MoonshineState& ms, int max_enc_frames,
                                        ggml_backend_t backend) {
    ms.dec_cache.Free();

    const int n_layers = hparams_.decoder_depth;
    const int n_heads = hparams_.decoder_heads;
    const int head_dim = hparams_.decoder_head_dim;
    const int max_seq = hparams_.max_seq_len;

    int n_tensors = n_layers * 4;
    size_t mem_size = n_tensors * ggml_tensor_overhead() + 1024;
    struct ggml_init_params params = { mem_size, nullptr, true };
    ms.dec_cache.ctx = ggml_init(params);
    if (!ms.dec_cache.ctx) {
        RS_LOG_ERR("Moonshine: failed to init decoder cache context");
        return false;
    }

    ms.dec_cache.self_k.resize(n_layers);
    ms.dec_cache.self_v.resize(n_layers);
    ms.dec_cache.cross_k.resize(n_layers);
    ms.dec_cache.cross_v.resize(n_layers);

    for (int l = 0; l < n_layers; l++) {
        char name[64];
        snprintf(name, sizeof(name), "dec_self_k_%d", l);
        ms.dec_cache.self_k[l] = ggml_new_tensor_3d(ms.dec_cache.ctx, GGML_TYPE_F32,
                                                      head_dim, n_heads, max_seq);
        ggml_set_name(ms.dec_cache.self_k[l], name);

        snprintf(name, sizeof(name), "dec_self_v_%d", l);
        ms.dec_cache.self_v[l] = ggml_new_tensor_3d(ms.dec_cache.ctx, GGML_TYPE_F32,
                                                      head_dim, n_heads, max_seq);
        ggml_set_name(ms.dec_cache.self_v[l], name);

        snprintf(name, sizeof(name), "dec_cross_k_%d", l);
        ms.dec_cache.cross_k[l] = ggml_new_tensor_3d(ms.dec_cache.ctx, GGML_TYPE_F32,
                                                       head_dim, n_heads, max_enc_frames);
        ggml_set_name(ms.dec_cache.cross_k[l], name);

        snprintf(name, sizeof(name), "dec_cross_v_%d", l);
        ms.dec_cache.cross_v[l] = ggml_new_tensor_3d(ms.dec_cache.ctx, GGML_TYPE_F32,
                                                       head_dim, n_heads, max_enc_frames);
        ggml_set_name(ms.dec_cache.cross_v[l], name);
    }

    ms.dec_cache.buf = ggml_backend_alloc_ctx_tensors(ms.dec_cache.ctx, backend);
    if (!ms.dec_cache.buf) {
        RS_LOG_ERR("Moonshine: failed to allocate decoder cache buffer");
        ms.dec_cache.Free();
        return false;
    }

    ggml_backend_buffer_clear(ms.dec_cache.buf, 0);
    ms.dec_cache.self_seq_len = 0;
    ms.dec_cache.cross_valid = false;
    ms.dec_cache.max_enc_frames = max_enc_frames;
    return true;
}

// ============================================================
// Cross K/V projection (computed once per utterance)
// ============================================================

bool MoonshineModel::ComputeCrossKV(MoonshineState& ms, ggml_backend_sched_t sched) {
    const int n_layers = hparams_.decoder_depth;
    const int n_heads = hparams_.decoder_heads;
    const int head_dim = hparams_.decoder_head_dim;
    const int enc_dim = hparams_.encoder_dim;
    const int enc_frames = ms.encoder_frames;

    const int n_nodes = 4096;
    struct ggml_context* ctx0 = nullptr;
    struct ggml_cgraph* gf = nullptr;
    if (!init_compute_ctx(&ctx0, &gf, n_nodes)) return false;

    ggml_tensor* enc_in = ggml_new_tensor_2d(ctx0, GGML_TYPE_F32, enc_dim, enc_frames);
    ggml_set_name(enc_in, "enc_for_cross");
    ggml_set_input(enc_in);

    std::vector<ggml_tensor*> ck_outs(n_layers);
    std::vector<ggml_tensor*> cv_outs(n_layers);
    for (int l = 0; l < n_layers; l++) {
        auto& layer = weights_.decoder_layers[l];
        ggml_tensor* ck = ggml_mul_mat(ctx0, layer.cross_attn_k_weight, enc_in);
        ggml_tensor* cv = ggml_mul_mat(ctx0, layer.cross_attn_v_weight, enc_in);
        ck = ggml_reshape_3d(ctx0, ck, head_dim, n_heads, enc_frames);
        cv = ggml_reshape_3d(ctx0, cv, head_dim, n_heads, enc_frames);

        char name[64];
        snprintf(name, sizeof(name), "ck_out_%d", l);
        ggml_set_name(ck, name); ggml_set_output(ck);
        snprintf(name, sizeof(name), "cv_out_%d", l);
        ggml_set_name(cv, name); ggml_set_output(cv);

        ck_outs[l] = ck;
        cv_outs[l] = cv;
        ggml_build_forward_expand(gf, ck);
        ggml_build_forward_expand(gf, cv);
    }

    ggml_backend_sched_reset(sched);
    if (!ggml_backend_sched_alloc_graph(sched, gf)) {
        RS_LOG_ERR("Moonshine: cross KV graph alloc failed");
        ggml_free(ctx0);
        return false;
    }

    ggml_backend_tensor_set(enc_in, ms.encoder_out.data(), 0,
                            ms.encoder_out.size() * sizeof(float));

    if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
        RS_LOG_ERR("Moonshine: cross KV graph compute failed");
        ggml_free(ctx0);
        return false;
    }

    // Copy results into persistent cache
    int kv_size = head_dim * n_heads * enc_frames;
    std::vector<float> tmp(kv_size);
    for (int l = 0; l < n_layers; l++) {
        ggml_backend_tensor_get(ck_outs[l], tmp.data(), 0, kv_size * sizeof(float));
        ggml_backend_tensor_set(ms.dec_cache.cross_k[l], tmp.data(), 0,
                                kv_size * sizeof(float));
        ggml_backend_tensor_get(cv_outs[l], tmp.data(), 0, kv_size * sizeof(float));
        ggml_backend_tensor_set(ms.dec_cache.cross_v[l], tmp.data(), 0,
                                kv_size * sizeof(float));
    }

    ms.dec_cache.cross_valid = true;
    ggml_free(ctx0);
    return true;
}

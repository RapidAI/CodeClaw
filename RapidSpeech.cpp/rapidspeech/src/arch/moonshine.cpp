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
    // Try standard GGUF key first, then legacy key
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
    // -> transpose to [seq, dim]

    // ggml conv1d expects input [length, in_channels]
    ggml_tensor* x = ggml_reshape_2d(ctx0, audio_input,
                                      (int)audio_input->ne[0], 1);

    // Helper: add bias [OC] to conv output [OL, OC]
    auto add_bias = [&](ggml_tensor* out, ggml_tensor* bias) -> ggml_tensor* {
        if (!bias) return out;
        ggml_tensor* b = ggml_reshape_2d(ctx0, bias, 1, (int)bias->ne[0]);
        return ggml_add(ctx0, out, b);
    };

    RS_LOG_INFO("BuildEncoder: input ne=[%lld], conv1_w ne=[%lld,%lld,%lld]",
        (long long)audio_input->ne[0],
        (long long)weights_.frontend_conv1_weight->ne[0],
        (long long)weights_.frontend_conv1_weight->ne[1],
        (long long)weights_.frontend_conv1_weight->ne[2]);

    // conv1: stride=64, no bias, Tanh
    // ggml conv_1d im2col requires kernel in F16
    x = ggml_conv_1d(ctx0, ggml_cast(ctx0, weights_.frontend_conv1_weight, GGML_TYPE_F16), x, 64, 0, 1);
    RS_LOG_INFO("BuildEncoder: after conv1, x ne=[%lld,%lld,%lld]",
        (long long)x->ne[0], (long long)x->ne[1], (long long)x->ne[2]);
    x = ggml_reshape_2d(ctx0, x, (int)x->ne[0], (int)x->ne[1]);
    x = add_bias(x, weights_.frontend_conv1_bias);
    x = ggml_tanh(ctx0, x);
    RS_LOG_INFO("BuildEncoder: after tanh, x ne=[%lld,%lld]", (long long)x->ne[0], (long long)x->ne[1]);

    // GroupNorm(1, dim): norm over channel dim
    // x is [OL, dim] after conv1. group_norm_1 handles the transpose internally.
    x = group_norm_1(ctx0, x, weights_.frontend_groupnorm_weight,
                     weights_.frontend_groupnorm_bias);
    RS_LOG_INFO("BuildEncoder: after groupnorm, x ne=[%lld,%lld]", (long long)x->ne[0], (long long)x->ne[1]);

    // conv2: stride=3, GELU
    x = ggml_conv_1d(ctx0, ggml_cast(ctx0, weights_.frontend_conv2_weight, GGML_TYPE_F16), x, 3, 0, 1);
    RS_LOG_INFO("BuildEncoder: after conv2 raw, x ne=[%lld,%lld,%lld]",
        (long long)x->ne[0], (long long)x->ne[1], (long long)x->ne[2]);
    x = ggml_reshape_2d(ctx0, x, (int)x->ne[0], (int)x->ne[1]);
    x = add_bias(x, weights_.frontend_conv2_bias);
    x = ggml_gelu(ctx0, x);
    RS_LOG_INFO("BuildEncoder: after conv2+gelu, x ne=[%lld,%lld]", (long long)x->ne[0], (long long)x->ne[1]);

    // conv3: stride=2, GELU
    x = ggml_conv_1d(ctx0, ggml_cast(ctx0, weights_.frontend_conv3_weight, GGML_TYPE_F16), x, 2, 0, 1);
    RS_LOG_INFO("BuildEncoder: after conv3 raw, x ne=[%lld,%lld,%lld]",
        (long long)x->ne[0], (long long)x->ne[1], (long long)x->ne[2]);
    x = ggml_reshape_2d(ctx0, x, (int)x->ne[0], (int)x->ne[1]);
    x = add_bias(x, weights_.frontend_conv3_bias);
    x = ggml_gelu(ctx0, x);
    RS_LOG_INFO("BuildEncoder: after conv3+gelu, x ne=[%lld,%lld]", (long long)x->ne[0], (long long)x->ne[1]);

    // Transpose to [dim, n_frames] for transformer (ggml matmul convention)
    int n_frames = (int)x->ne[0];
    x = ggml_cont(ctx0, ggml_transpose(ctx0, x));
    RS_LOG_INFO("BuildEncoder: after transpose, x ne=[%lld,%lld], n_frames=%d", (long long)x->ne[0], (long long)x->ne[1], n_frames);

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

        // RoPE (expects ne[2]=seq to match position count)
        q = apply_rope(ctx0, q, enc_positions, head_dim, hparams_.rope_theta);
        k = apply_rope(ctx0, k, enc_positions, head_dim, hparams_.rope_theta);

        // Manual scaled dot-product attention in F32
        // Permute to [head_dim, n_frames, n_heads] for batched matmul over heads
        float scale = 1.0f / sqrtf((float)head_dim);
        q = ggml_scale(ctx0, q, scale);
        ggml_tensor* qp = ggml_cont(ctx0, ggml_permute(ctx0, q, 0, 2, 1, 3));
        ggml_tensor* kp = ggml_cont(ctx0, ggml_permute(ctx0, k, 0, 2, 1, 3));
        ggml_tensor* vp = ggml_cont(ctx0, ggml_permute(ctx0, v, 0, 2, 1, 3));

        // scores = kp^T @ qp -> [n_frames, n_frames, n_heads]
        ggml_tensor* scores = ggml_mul_mat(ctx0, kp, qp);
        scores = ggml_soft_max(ctx0, scores);

        // attn = vp_t^T @ scores where vp_t = permute(vp, 1,0,2,3) = [n_frames, head_dim, n_heads]
        // result: [head_dim, n_frames, n_heads]
        ggml_tensor* vp_t = ggml_cont(ctx0, ggml_permute(ctx0, vp, 1, 0, 2, 3));
        ggml_tensor* attn_out = ggml_mul_mat(ctx0, vp_t, scores);

        // Permute back to [head_dim, n_heads, n_frames] then reshape to [dim, n_frames]
        attn_out = ggml_cont(ctx0, ggml_permute(ctx0, attn_out, 0, 2, 1, 3));
        attn_out = ggml_reshape_2d(ctx0, attn_out, dim, n_frames);
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

    RS_LOG_INFO("Moonshine: building encoder graph for %d samples...", n_samples);
    ggml_tensor* enc_positions_out = nullptr;
    ggml_tensor* enc_out = BuildEncoder(ctx0, audio_in, n_samples, false,
                                         &enc_positions_out);
    if (!enc_out) {
        RS_LOG_ERR("Moonshine: BuildEncoder returned null");
        ggml_free(ctx0);
        return false;
    }
    RS_LOG_INFO("Moonshine: encoder graph built, enc_out ne=[%lld,%lld]",
                (long long)enc_out->ne[0], (long long)enc_out->ne[1]);
    ggml_set_name(enc_out, "encoder_output");
    ggml_set_output(enc_out);
    ggml_build_forward_expand(gf, enc_out);

    RS_LOG_INFO("Moonshine: allocating encoder graph...");
    if (!ggml_backend_sched_alloc_graph(sched, gf)) {
        RS_LOG_ERR("Moonshine: encoder graph allocation failed");
        ggml_free(ctx0);
        return false;
    }
    RS_LOG_INFO("Moonshine: setting input data...");
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

    RS_LOG_INFO("Moonshine: computing encoder graph...");
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

    // Check for NaN in encoder output
    {
        bool has_nan = false;
        float sum = 0;
        for (size_t i = 0; i < ms.encoder_out.size(); i++) {
            if (std::isnan(ms.encoder_out[i])) { has_nan = true; break; }
            sum += ms.encoder_out[i];
        }
        RS_LOG_INFO("Moonshine: encoder output check: has_nan=%d, mean=%.6f",
                    has_nan, sum / ms.encoder_out.size());
    }

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

// ============================================================
// Single decoder step with pre-allocated KV cache
//
// Decoder uses:
//   - RMSNorm (not LayerNorm)
//   - SwiGLU FFN: fc1 outputs 2*intermediate, split into gate+value
//     gate = silu(first_half), output = gate * second_half
//   - Causal self-attention with KV cache
//   - Cross-attention from persistent cache
// ============================================================

int MoonshineModel::RunDecoderStep(MoonshineState& ms, int step, int cur_token,
                                    ggml_backend_sched_t sched) {
    const int dim = hparams_.decoder_dim;
    const int n_layers = hparams_.decoder_depth;
    const int n_heads = hparams_.decoder_heads;
    const int head_dim = hparams_.decoder_head_dim;
    const int enc_frames = ms.encoder_frames;
    const int kv_entry_floats = head_dim * n_heads;

    const int n_nodes = 65536;
    struct ggml_context* ctx0 = nullptr;
    struct ggml_cgraph* gf = nullptr;
    if (!init_compute_ctx(&ctx0, &gf, n_nodes)) return -1;

    // Inputs
    ggml_tensor* tok_idx = ggml_new_tensor_1d(ctx0, GGML_TYPE_I32, 1);
    ggml_set_name(tok_idx, "token_id");
    ggml_set_input(tok_idx);

    ggml_tensor* rope_pos = ggml_new_tensor_1d(ctx0, GGML_TYPE_I32, 1);
    ggml_set_name(rope_pos, "rope_pos");
    ggml_set_input(rope_pos);

    // Token embedding
    ggml_tensor* x = ggml_get_rows(ctx0, weights_.token_embedding, tok_idx);

    // Collect new K/V for readback
    std::vector<ggml_tensor*> new_self_k(n_layers);
    std::vector<ggml_tensor*> new_self_v(n_layers);

    for (int l = 0; l < n_layers; l++) {
        auto& layer = weights_.decoder_layers[l];
        float scale = 1.0f / sqrtf((float)head_dim);

        // --- Self-attention (RMSNorm + causal + KV cache) ---
        ggml_tensor* residual = x;
        x = rms_norm(ctx0, x, layer.self_attn_norm_weight);

        ggml_tensor* q     = ggml_mul_mat(ctx0, layer.self_attn_q_weight, x);
        ggml_tensor* k_new = ggml_mul_mat(ctx0, layer.self_attn_k_weight, x);
        ggml_tensor* v_new = ggml_mul_mat(ctx0, layer.self_attn_v_weight, x);

        q     = ggml_reshape_3d(ctx0, q,     head_dim, n_heads, 1);
        k_new = ggml_reshape_3d(ctx0, k_new, head_dim, n_heads, 1);
        v_new = ggml_reshape_3d(ctx0, v_new, head_dim, n_heads, 1);

        // RoPE with explicit position (expects [head_dim, n_heads, seq] with ne[2]=seq)
        q     = apply_rope(ctx0, q,     rope_pos, head_dim, hparams_.rope_theta);
        k_new = apply_rope(ctx0, k_new, rope_pos, head_dim, hparams_.rope_theta);

        // Permute to [head_dim, seq, n_heads] for flash_attn_ext
        q     = ggml_cont(ctx0, ggml_permute(ctx0, q,     0, 2, 1, 3));
        k_new = ggml_cont(ctx0, ggml_permute(ctx0, k_new, 0, 2, 1, 3));
        v_new = ggml_cont(ctx0, ggml_permute(ctx0, v_new, 0, 2, 1, 3));

        // Mark new k/v for readback (stored in [head_dim, 1, n_heads] layout)
        char name[64];
        snprintf(name, sizeof(name), "new_sk_%d", l);
        ggml_set_name(k_new, name); ggml_set_output(k_new);
        snprintf(name, sizeof(name), "new_sv_%d", l);
        ggml_set_name(v_new, name); ggml_set_output(v_new);
        new_self_k[l] = k_new;
        new_self_v[l] = v_new;

        // Build full K/V for attention: [head_dim, seq, n_heads]
        // Cache is stored as [head_dim, n_heads, max_seq], need permute
        ggml_tensor* k_full;
        ggml_tensor* v_full;
        if (step > 0) {
            size_t nb1 = head_dim * sizeof(float);
            size_t nb2 = head_dim * n_heads * sizeof(float);
            ggml_tensor* k_cached = ggml_view_3d(ctx0, ms.dec_cache.self_k[l],
                                                  head_dim, n_heads, step,
                                                  nb1, nb2, 0);
            ggml_tensor* v_cached = ggml_view_3d(ctx0, ms.dec_cache.self_v[l],
                                                  head_dim, n_heads, step,
                                                  nb1, nb2, 0);
            // Permute cached from [head_dim, n_heads, step] to [head_dim, step, n_heads]
            k_cached = ggml_cont(ctx0, ggml_permute(ctx0, k_cached, 0, 2, 1, 3));
            v_cached = ggml_cont(ctx0, ggml_permute(ctx0, v_cached, 0, 2, 1, 3));
            // Concat along seq dim (dim 1): [head_dim, step+1, n_heads]
            k_full = ggml_concat(ctx0, k_cached, k_new, 1);
            v_full = ggml_concat(ctx0, v_cached, v_new, 1);
        } else {
            k_full = k_new;
            v_full = v_new;
        }

        if (l == 0) RS_LOG_INFO("Dec step %d layer %d: self q=[%lld,%lld,%lld] k=[%lld,%lld,%lld]",
            step, l, (long long)q->ne[0], (long long)q->ne[1], (long long)q->ne[2],
            (long long)k_full->ne[0], (long long)k_full->ne[1], (long long)k_full->ne[2]);
        ggml_tensor* self_attn = ggml_flash_attn_ext(ctx0,
                                    ggml_cast(ctx0, q, GGML_TYPE_F16),
                                    ggml_cast(ctx0, k_full, GGML_TYPE_F16),
                                    ggml_cast(ctx0, v_full, GGML_TYPE_F16),
                                    nullptr, scale, 0.0f, 0.0f);
        ggml_flash_attn_ext_set_prec(self_attn, GGML_PREC_F32);
        self_attn = ggml_reshape_2d(ctx0, self_attn, dim, 1);
        self_attn = ggml_mul_mat(ctx0, layer.self_attn_out_weight, self_attn);
        x = ggml_add(ctx0, residual, self_attn);

        // --- Cross-attention (RMSNorm + persistent cross KV) ---
        residual = x;
        x = rms_norm(ctx0, x, layer.cross_attn_norm_weight);

        ggml_tensor* cq = ggml_mul_mat(ctx0, layer.cross_attn_q_weight, x);
        // Reshape to [head_dim, n_heads, 1] then permute to [head_dim, 1, n_heads]
        cq = ggml_reshape_3d(ctx0, cq, head_dim, n_heads, 1);
        cq = ggml_cont(ctx0, ggml_permute(ctx0, cq, 0, 2, 1, 3));

        // Cross K/V from cache: [head_dim, n_heads, enc_frames] -> permute to [head_dim, enc_frames, n_heads]
        size_t cnb1 = head_dim * sizeof(float);
        size_t cnb2 = head_dim * n_heads * sizeof(float);
        ggml_tensor* ck = ggml_view_3d(ctx0, ms.dec_cache.cross_k[l],
                                        head_dim, n_heads, enc_frames,
                                        cnb1, cnb2, 0);
        ggml_tensor* cv = ggml_view_3d(ctx0, ms.dec_cache.cross_v[l],
                                        head_dim, n_heads, enc_frames,
                                        cnb1, cnb2, 0);
        ck = ggml_cont(ctx0, ggml_permute(ctx0, ck, 0, 2, 1, 3));
        cv = ggml_cont(ctx0, ggml_permute(ctx0, cv, 0, 2, 1, 3));

        if (l == 0) RS_LOG_INFO("Dec step %d layer %d: cross cq=[%lld,%lld,%lld] ck=[%lld,%lld,%lld]",
            step, l, (long long)cq->ne[0], (long long)cq->ne[1], (long long)cq->ne[2],
            (long long)ck->ne[0], (long long)ck->ne[1], (long long)ck->ne[2]);
        ggml_tensor* cross_attn = ggml_flash_attn_ext(ctx0,
                                    ggml_cast(ctx0, cq, GGML_TYPE_F16),
                                    ggml_cast(ctx0, ck, GGML_TYPE_F16),
                                    ggml_cast(ctx0, cv, GGML_TYPE_F16),
                                    nullptr, scale, 0.0f, 0.0f);
        ggml_flash_attn_ext_set_prec(cross_attn, GGML_PREC_F32);
        if (l == 0) RS_LOG_INFO("Dec step %d: cross attn built OK", step);
        cross_attn = ggml_reshape_2d(ctx0, cross_attn, dim, 1);
        cross_attn = ggml_mul_mat(ctx0, layer.cross_attn_out_weight, cross_attn);
        x = ggml_add(ctx0, residual, cross_attn);
        if (l == 0) RS_LOG_INFO("Dec step %d: entering FFN", step);

        // --- FFN: RMSNorm + SwiGLU ---
        // fc1 outputs [2*intermediate, 1]. Split into gate and value halves.
        // gate = silu(first_half), output = gate * second_half, then fc2.
        residual = x;
        x = rms_norm(ctx0, x, layer.ff_norm_weight);

        ggml_tensor* fc1_out = ggml_mul_mat(ctx0, layer.ff_up_weight, x);
        if (layer.ff_up_bias) {
            ggml_tensor* bias_2d = ggml_reshape_2d(ctx0, layer.ff_up_bias,
                                                    (int)layer.ff_up_bias->ne[0], 1);
            fc1_out = ggml_add(ctx0, fc1_out, bias_2d);
        }
        fc1_out = ggml_cont(ctx0, fc1_out);  // ensure contiguous for view
        if (l == 0) RS_LOG_INFO("Dec step %d: FFN fc1 done, ne=[%lld,%lld]", step,
            (long long)fc1_out->ne[0], (long long)fc1_out->ne[1]);

        int intermediate_2x = (int)fc1_out->ne[0];
        int intermediate = intermediate_2x / 2;

        // SwiGLU: reshape [2*inter, 1] -> [inter, 2], take columns
        ggml_tensor* fc1_2col = ggml_reshape_2d(ctx0, fc1_out, intermediate, 2);
        ggml_tensor* gate_part = ggml_view_1d(ctx0, fc1_out, intermediate, 0);
        gate_part = ggml_reshape_2d(ctx0, gate_part, intermediate, 1);
        ggml_tensor* value_part = ggml_view_1d(ctx0, fc1_out, intermediate,
                                                intermediate * sizeof(float));
        value_part = ggml_reshape_2d(ctx0, value_part, intermediate, 1);

        gate_part = ggml_silu(ctx0, gate_part);
        x = ggml_mul(ctx0, gate_part, value_part);
        if (l == 0) RS_LOG_INFO("Dec step %d: FFN swiglu done, x ne=[%lld,%lld]", step,
            (long long)x->ne[0], (long long)x->ne[1]);

        x = ggml_mul_mat(ctx0, layer.ff_down_weight, x);
        if (l == 0) RS_LOG_INFO("Dec step %d: FFN fc2 done", step);
        if (layer.ff_down_bias) {
            ggml_tensor* dbias_2d = ggml_reshape_2d(ctx0, layer.ff_down_bias,
                                                     (int)layer.ff_down_bias->ne[0], 1);
            x = ggml_add(ctx0, x, dbias_2d);
        }
        if (l == 0) RS_LOG_INFO("Dec step %d: FFN bias done", step);
        x = ggml_add(ctx0, residual, x);
        if (l == 0) RS_LOG_INFO("Dec step %d: layer 0 complete", step);
    }
    RS_LOG_INFO("Dec step %d: all %d layers done", step, n_layers);

    // Final norm + LM head
    x = rms_norm(ctx0, x, weights_.decoder_final_norm_weight);
    // Use lm_head_weight if available, otherwise use token_embedding (weight tying)
    ggml_tensor* lm_weight = weights_.lm_head_weight ? weights_.lm_head_weight
                                                      : weights_.token_embedding;
    x = ggml_mul_mat(ctx0, lm_weight, x);
    if (weights_.lm_head_bias) x = ggml_add(ctx0, x, weights_.lm_head_bias);
    RS_LOG_INFO("Dec step %d: lm_head done, x ne=[%lld,%lld]", step, (long long)x->ne[0], (long long)x->ne[1]);

    ggml_set_name(x, "logits");
    ggml_set_output(x);
    ggml_build_forward_expand(gf, x);
    for (int l = 0; l < n_layers; l++) {
        ggml_build_forward_expand(gf, new_self_k[l]);
        ggml_build_forward_expand(gf, new_self_v[l]);
    }

    // Allocate and set inputs
    ggml_backend_sched_reset(sched);
    if (!ggml_backend_sched_alloc_graph(sched, gf)) {
        RS_LOG_ERR("Moonshine: decoder step %d alloc failed", step);
        ggml_free(ctx0);
        return -1;
    }

    ggml_backend_tensor_set(tok_idx, &cur_token, 0, sizeof(int32_t));
    int32_t step_i32 = (int32_t)step;
    ggml_backend_tensor_set(rope_pos, &step_i32, 0, sizeof(int32_t));

    if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
        RS_LOG_ERR("Moonshine: decoder step %d compute failed", step);
        ggml_free(ctx0);
        return -1;
    }

    // Write new self K/V to persistent cache at position [step]
    std::vector<float> kv_tmp(kv_entry_floats);
    size_t offset = (size_t)step * kv_entry_floats * sizeof(float);
    for (int l = 0; l < n_layers; l++) {
        ggml_backend_tensor_get(new_self_k[l], kv_tmp.data(), 0,
                                kv_entry_floats * sizeof(float));
        ggml_backend_tensor_set(ms.dec_cache.self_k[l], kv_tmp.data(),
                                offset, kv_entry_floats * sizeof(float));

        ggml_backend_tensor_get(new_self_v[l], kv_tmp.data(), 0,
                                kv_entry_floats * sizeof(float));
        ggml_backend_tensor_set(ms.dec_cache.self_v[l], kv_tmp.data(),
                                offset, kv_entry_floats * sizeof(float));
    }
    ms.dec_cache.self_seq_len = step + 1;

    // Greedy argmax
    std::vector<float> logit_buf(hparams_.vocab_size);
    ggml_backend_tensor_get(x, logit_buf.data(), 0,
                            hparams_.vocab_size * sizeof(float));

    if (step == 0) {
        RS_LOG_INFO("Dec step 0 logits[0..4]: %.4f %.4f %.4f %.4f %.4f",
            logit_buf[0], logit_buf[1], logit_buf[2], logit_buf[3], logit_buf[4]);
    }

    int best_id = 0;
    float best_val = logit_buf[0];
    for (int i = 1; i < hparams_.vocab_size; i++) {
        if (logit_buf[i] > best_val) {
            best_val = logit_buf[i];
            best_id = i;
        }
    }

    ggml_free(ctx0);
    return best_id;
}

// ============================================================
// Decode: autoregressive generation with persistent KV cache
// ============================================================

bool MoonshineModel::Decode(RSState& state, ggml_backend_sched_t sched) {
    auto& ms = dynamic_cast<MoonshineState&>(state);

    if (ms.encoder_out.empty() || ms.encoder_frames == 0) {
        RS_LOG_ERR("Moonshine: no encoder output for decoding");
        return false;
    }

    const int max_len = hparams_.max_seq_len;
    const int enc_frames = ms.encoder_frames;

    ggml_backend_t backend = ggml_backend_sched_get_backend(sched, 0);

    if (!AllocDecoderCache(ms, enc_frames, backend)) return false;
    if (!ComputeCrossKV(ms, sched)) return false;

    ms.tokens.clear();
    ms.tokens.push_back(hparams_.bos_id);

    for (int step = 0; step < max_len; step++) {
        int cur_token = ms.tokens.back();
        int next_id = RunDecoderStep(ms, step, cur_token, sched);
        if (next_id < 0) return false;
        if (next_id == hparams_.eos_id) break;
        ms.tokens.push_back(next_id);
    }

    RS_LOG_INFO("Moonshine: decoded %zu tokens", ms.tokens.size());
    return true;
}

// ============================================================
// GetTranscription: token IDs -> text
// ============================================================

std::string MoonshineModel::GetTranscription(RSState& state) {
    auto& ms = dynamic_cast<MoonshineState&>(state);

    std::ostringstream oss;
    for (size_t i = 0; i < ms.tokens.size(); i++) {
        int tid = ms.tokens[i];
        if (tid == hparams_.bos_id || tid == hparams_.eos_id) continue;
        auto it = vocab_.find(tid);
        if (it != vocab_.end()) {
            oss << it->second;
        } else {
            oss << "<" << tid << ">";
        }
    }

    ms.text_result = oss.str();
    return ms.text_result;
}

// ============================================================
// Streaming encoder: incremental encoding
// ============================================================

int MoonshineModel::PushStreamingAudio(RSState& state, const float* audio,
                                        int n_samples,
                                        ggml_backend_sched_t sched) {
    auto& ms = dynamic_cast<MoonshineState&>(state);

    ms.sample_buffer.insert(ms.sample_buffer.end(), audio, audio + n_samples);

    int chunk_size = hparams_.sample_rate;
    if (hparams_.is_streaming) {
        chunk_size = hparams_.frame_len * 10;
        if (chunk_size < 160) chunk_size = 160;
    }

    int new_frames = 0;

    while ((int)ms.sample_buffer.size() >= chunk_size) {
        std::vector<float> chunk(ms.sample_buffer.begin(),
                                  ms.sample_buffer.begin() + chunk_size);
        ms.sample_buffer.erase(ms.sample_buffer.begin(),
                                ms.sample_buffer.begin() + chunk_size);

        if (hparams_.is_streaming) {
            const int n_nodes = 16384;
            struct ggml_context* ctx0 = nullptr;
            struct ggml_cgraph* gf = nullptr;
            if (!init_compute_ctx(&ctx0, &gf, n_nodes)) break;

            int n_chunk = (int)chunk.size();
            ggml_tensor* audio_in = ggml_new_tensor_1d(ctx0, GGML_TYPE_F32, n_chunk);
            ggml_set_name(audio_in, "stream_pcm");
            ggml_set_input(audio_in);

            ggml_tensor* enc_out = BuildEncoder(ctx0, audio_in, n_chunk, true, nullptr);
            if (!enc_out) { ggml_free(ctx0); break; }
            ggml_set_name(enc_out, "stream_enc_out");
            ggml_set_output(enc_out);
            ggml_build_forward_expand(gf, enc_out);

            ggml_backend_sched_reset(sched);
            if (!ggml_backend_sched_alloc_graph(sched, gf)) {
                RS_LOG_ERR("Moonshine: streaming encoder alloc failed");
                ggml_free(ctx0);
                break;
            }

            ggml_backend_tensor_set(audio_in, chunk.data(), 0,
                                    n_chunk * sizeof(float));

            if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
                RS_LOG_ERR("Moonshine: streaming encoder compute failed");
                ggml_free(ctx0);
                break;
            }

            int enc_dim = hparams_.encoder_dim;
            int chunk_frames = (int)(ggml_nelements(enc_out) / enc_dim);

            std::vector<float> chunk_enc(enc_dim * chunk_frames);
            ggml_backend_tensor_get(enc_out, chunk_enc.data(), 0,
                                    chunk_enc.size() * sizeof(float));

            ms.encoder_out.insert(ms.encoder_out.end(),
                                   chunk_enc.begin(), chunk_enc.end());
            ms.encoder_frames += chunk_frames;
            ms.streaming_enc_frames += chunk_frames;
            new_frames += chunk_frames;

            ggml_free(ctx0);
        } else {
            // Non-streaming fallback: re-encode accumulated audio
            std::vector<float> prev_enc = ms.encoder_out;
            int prev_frames = ms.encoder_frames;

            ggml_backend_sched_reset(sched);
            if (Encode(chunk, state, sched)) {
                if (prev_frames > 0) {
                    std::vector<float> combined;
                    combined.reserve(prev_enc.size() + ms.encoder_out.size());
                    combined.insert(combined.end(), prev_enc.begin(), prev_enc.end());
                    combined.insert(combined.end(), ms.encoder_out.begin(),
                                    ms.encoder_out.end());
                    ms.encoder_out = std::move(combined);
                    ms.encoder_frames = prev_frames + ms.encoder_frames;
                }
                new_frames += ms.encoder_frames - prev_frames;
            }
        }
    }

    return new_frames;
}

// ============================================================
// BuildCausalEncoder stub (reserved for sliding-window)
// ============================================================

ggml_tensor* MoonshineModel::BuildCausalEncoder(ggml_context* /*ctx0*/,
                                                 ggml_tensor* /*audio_features*/,
                                                 int /*n_new_frames*/,
                                                 int /*n_prev_frames*/) {
    // TODO: sliding-window causal encoder with conv state carry-over
    return nullptr;
}

// ============================================================
// Auto-register architecture
// ============================================================

static struct MoonshineRegistrar {
    MoonshineRegistrar() {
        rs_register_model_arch("moonshine", []() -> std::shared_ptr<ISpeechModel> {
            return std::make_shared<MoonshineModel>();
        });
    }
} s_moonshine_registrar;

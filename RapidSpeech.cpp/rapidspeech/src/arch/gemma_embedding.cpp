#include "arch/gemma_embedding.h"
#include "core/rs_context.h"
#include "utils/rs_log.h"
#include "ggml.h"
#include "ggml-backend.h"
#include "ggml-alloc.h"
#include "gguf.h"

#include <cmath>
#include <cstring>
#include <algorithm>
#include <numeric>

// ============================================================
// Gemma 300M Embedding — ggml native
//
// Architecture: Gemma-style transformer (RMSNorm, RoPE, SiLU-gated FFN)
// Output: mean-pooled hidden states -> L2 normalized embedding
// Supports MRL truncation (768 -> 512/256/128)
// ============================================================

GemmaEmbeddingModel::GemmaEmbeddingModel() {
    meta_.arch_name = "gemma-embedding";
    meta_.audio_sample_rate = 0;  // text-only model
    meta_.n_mels = 0;
    meta_.vocab_size = 256000;
}

GemmaEmbeddingModel::~GemmaEmbeddingModel() = default;

bool GemmaEmbeddingModel::MapTensors(ggml_context* gguf_data) {
    auto get = [&](const char* name) -> ggml_tensor* {
        return ggml_get_tensor(gguf_data, name);
    };

    weights_.token_embedding = get("model.embed_tokens.weight");

    weights_.layers.resize(hparams_.n_layers);
    for (int i = 0; i < hparams_.n_layers; i++) {
        auto& layer = weights_.layers[i];
        char buf[256];
        auto gn = [&](const char* suffix) -> ggml_tensor* {
            snprintf(buf, sizeof(buf), "model.layers.%d.%s", i, suffix);
            return get(buf);
        };

        layer.attn_q_weight   = gn("self_attn.q_proj.weight");
        layer.attn_k_weight   = gn("self_attn.k_proj.weight");
        layer.attn_v_weight   = gn("self_attn.v_proj.weight");
        layer.attn_out_weight = gn("self_attn.o_proj.weight");
        layer.attn_norm_weight = gn("input_layernorm.weight");

        layer.ff_gate_weight = gn("mlp.gate_proj.weight");
        layer.ff_up_weight   = gn("mlp.up_proj.weight");
        layer.ff_down_weight = gn("mlp.down_proj.weight");
        layer.ff_norm_weight = gn("post_attention_layernorm.weight");
    }

    weights_.final_norm_weight = get("model.norm.weight");

    if (!weights_.token_embedding) {
        RS_LOG_ERR("GemmaEmb: token embedding missing");
        return false;
    }
    return true;
}

bool GemmaEmbeddingModel::LoadVocab(gguf_context* ctx_gguf) {
    int64_t key = gguf_find_key(ctx_gguf, "tokenizer.tokens");
    if (key < 0) {
        RS_LOG_WARN("GemmaEmb: no tokenizer.tokens in GGUF");
        return true;
    }
    int n = gguf_get_arr_n(ctx_gguf, key);
    for (int i = 0; i < n; i++) {
        const char* tok = gguf_get_arr_str(ctx_gguf, key, i);
        if (tok) vocab_[i] = std::string(tok);
    }
    RS_LOG_INFO("GemmaEmb: loaded %d vocab tokens", n);
    return true;
}

bool GemmaEmbeddingModel::Load(const std::unique_ptr<rs_context_t>& ctx,
                                ggml_backend_t /*backend*/) {
    if (!ctx || !ctx->gguf_data || !ctx->ctx_gguf) return false;

    auto ri = [&](const char* key, int def) -> int {
        int64_t k = gguf_find_key(ctx->ctx_gguf, key);
        return (k >= 0) ? gguf_get_val_i32(ctx->ctx_gguf, k) : def;
    };
    auto rf = [&](const char* key, float def) -> float {
        int64_t k = gguf_find_key(ctx->ctx_gguf, key);
        return (k >= 0) ? gguf_get_val_f32(ctx->ctx_gguf, k) : def;
    };

    hparams_.hidden_dim       = ri("gemma.hidden_size", 768);
    hparams_.n_layers         = ri("gemma.num_hidden_layers", 18);
    hparams_.n_heads          = ri("gemma.num_attention_heads", 12);
    hparams_.head_dim         = ri("gemma.head_dim", 64);
    hparams_.intermediate_dim = ri("gemma.intermediate_size", 3072);
    hparams_.vocab_size       = ri("gemma.vocab_size", 256000);
    hparams_.max_seq_len      = ri("gemma.max_position_embeddings", 512);
    hparams_.output_dim       = ri("gemma.output_dim", 768);
    hparams_.rms_norm_eps     = rf("gemma.rms_norm_eps", 1e-6f);
    hparams_.rope_theta       = rf("gemma.rope_theta", 10000.0f);

    meta_.vocab_size = hparams_.vocab_size;

    if (!MapTensors(ctx->gguf_data)) return false;
    if (!LoadVocab(ctx->ctx_gguf)) return false;

    RS_LOG_INFO("GemmaEmb loaded: dim=%d layers=%d heads=%d vocab=%d out=%d",
                hparams_.hidden_dim, hparams_.n_layers, hparams_.n_heads,
                hparams_.vocab_size, hparams_.output_dim);
    return true;
}

std::shared_ptr<RSState> GemmaEmbeddingModel::CreateState() {
    return std::make_shared<GemmaEmbState>();
}

// RMS normalization helper
static ggml_tensor* rms_norm(ggml_context* ctx0, ggml_tensor* x,
                              ggml_tensor* weight, float eps) {
    x = ggml_rms_norm(ctx0, x, eps);
    if (weight) x = ggml_mul(ctx0, x, weight);
    return x;
}

ggml_tensor* GemmaEmbeddingModel::BuildTransformer(ggml_context* ctx0,
                                                     ggml_tensor* x,
                                                     int seq_len) {
    const int dim = hparams_.hidden_dim;
    const int n_heads = hparams_.n_heads;
    const int head_dim = hparams_.head_dim;
    const float eps = hparams_.rms_norm_eps;

    for (int l = 0; l < hparams_.n_layers; l++) {
        auto& layer = weights_.layers[l];

        // Self-attention with RMSNorm
        ggml_tensor* residual = x;
        x = rms_norm(ctx0, x, layer.attn_norm_weight, eps);

        ggml_tensor* q = ggml_mul_mat(ctx0, layer.attn_q_weight, x);
        ggml_tensor* k = ggml_mul_mat(ctx0, layer.attn_k_weight, x);
        ggml_tensor* v = ggml_mul_mat(ctx0, layer.attn_v_weight, x);

        q = ggml_reshape_3d(ctx0, q, head_dim, n_heads, seq_len);
        k = ggml_reshape_3d(ctx0, k, head_dim, n_heads, seq_len);
        v = ggml_reshape_3d(ctx0, v, head_dim, n_heads, seq_len);

        // RoPE
        q = ggml_rope_ext(ctx0, q, nullptr, nullptr, head_dim, 0, 0,
                           hparams_.rope_theta, 1.0f, 0.0f, 1.0f, 0.0f, 0.0f);
        k = ggml_rope_ext(ctx0, k, nullptr, nullptr, head_dim, 0, 0,
                           hparams_.rope_theta, 1.0f, 0.0f, 1.0f, 0.0f, 0.0f);

        float scale = 1.0f / sqrtf((float)head_dim);
        ggml_tensor* attn = ggml_flash_attn_ext(ctx0, q, k, v, nullptr, scale, 0.0f, 0.0f);
        attn = ggml_reshape_2d(ctx0, attn, dim, seq_len);
        attn = ggml_mul_mat(ctx0, layer.attn_out_weight, attn);
        x = ggml_add(ctx0, residual, attn);

        // FFN with RMSNorm (SiLU-gated)
        residual = x;
        x = rms_norm(ctx0, x, layer.ff_norm_weight, eps);

        ggml_tensor* gate = ggml_mul_mat(ctx0, layer.ff_gate_weight, x);
        gate = ggml_silu(ctx0, gate);
        ggml_tensor* up = ggml_mul_mat(ctx0, layer.ff_up_weight, x);
        x = ggml_mul(ctx0, gate, up);
        x = ggml_mul_mat(ctx0, layer.ff_down_weight, x);
        x = ggml_add(ctx0, residual, x);
    }

    x = rms_norm(ctx0, x, weights_.final_norm_weight, eps);
    return x;
}

bool GemmaEmbeddingModel::EncodeTokens(const std::vector<int>& tokens,
                                        RSState& state,
                                        ggml_backend_sched_t sched) {
    auto& gs = dynamic_cast<GemmaEmbState&>(state);
    gs.token_ids = tokens;

    int seq_len = (int)tokens.size();
    if (seq_len == 0) return false;
    if (seq_len > hparams_.max_seq_len) seq_len = hparams_.max_seq_len;

    const int dim = hparams_.hidden_dim;

    const int n_nodes = 4096;
    struct ggml_context* ctx0 = nullptr;
    struct ggml_cgraph* gf = nullptr;
    if (!init_compute_ctx(&ctx0, &gf, n_nodes)) return false;

    // Token indices input
    ggml_tensor* tok_ids = ggml_new_tensor_1d(ctx0, GGML_TYPE_I32, seq_len);
    ggml_set_name(tok_ids, "token_ids");
    ggml_set_input(tok_ids);

    // Embedding lookup
    ggml_tensor* x = ggml_get_rows(ctx0, weights_.token_embedding, tok_ids);

    // Gemma scales embeddings by sqrt(dim)
    float emb_scale = sqrtf((float)dim);
    x = ggml_scale(ctx0, x, emb_scale);

    // Transformer
    x = BuildTransformer(ctx0, x, seq_len);

    // Mean pooling over sequence dimension
    // x is [dim, seq_len] — we want the mean across seq_len for each dim
    // ggml_mean computes mean of all elements -> scalar, not what we want.
    // Instead: repeat-add approach using ggml_cont + ggml_reshape
    // Simple approach: just read all [dim, seq_len] and do mean on CPU side
    // by reading the full output and averaging in the readback.
    // Mark the full transformer output for readback.
    x = ggml_cont(ctx0, x);
    x = ggml_reshape_2d(ctx0, x, dim, seq_len);

    ggml_set_name(x, "embedding_out");
    ggml_set_output(x);
    ggml_build_forward_expand(gf, x);

    if (!ggml_backend_sched_alloc_graph(sched, gf)) {
        RS_LOG_ERR("GemmaEmb: graph alloc failed");
        ggml_free(ctx0);
        return false;
    }

    // Set token IDs
    std::vector<int32_t> ids32(tokens.begin(), tokens.begin() + seq_len);
    ggml_backend_tensor_set(tok_ids, ids32.data(), 0, seq_len * sizeof(int32_t));

    if (!ggml_backend_sched_graph_compute(sched, gf)) {
        RS_LOG_ERR("GemmaEmb: graph compute failed");
        ggml_free(ctx0);
        return false;
    }

    // Read full [dim, seq_len] output and do mean pooling on CPU
    int out_dim = std::min(dim, hparams_.output_dim);
    std::vector<float> full_out(dim * seq_len);
    ggml_backend_tensor_get(x, full_out.data(), 0, dim * seq_len * sizeof(float));

    // Mean pool: average across seq_len for each dim element
    // Layout: [dim, seq_len] in row-major = dim is the fast dimension
    gs.embedding.resize(out_dim, 0.0f);
    for (int s = 0; s < seq_len; s++) {
        for (int d = 0; d < out_dim; d++) {
            gs.embedding[d] += full_out[s * dim + d];
        }
    }
    float inv_seq = 1.0f / (float)seq_len;
    for (int d = 0; d < out_dim; d++) {
        gs.embedding[d] *= inv_seq;
    }
    gs.emb_dim = out_dim;

    // L2 normalize
    float norm = 0.0f;
    for (int i = 0; i < out_dim; i++) norm += gs.embedding[i] * gs.embedding[i];
    norm = sqrtf(norm);
    if (norm > 1e-8f) {
        for (int i = 0; i < out_dim; i++) gs.embedding[i] /= norm;
    }

    ggml_free(ctx0);
    return true;
}

// Encode is a no-op for text embedding model (use EncodeTokens instead)
bool GemmaEmbeddingModel::Encode(const std::vector<float>& /*input_frames*/,
                                  RSState& /*state*/,
                                  ggml_backend_sched_t /*sched*/) {
    RS_LOG_WARN("GemmaEmb: Encode() called but this is a text model. Use EncodeTokens().");
    return false;
}

bool GemmaEmbeddingModel::Decode(RSState& /*state*/, ggml_backend_sched_t /*sched*/) {
    // No decode step for embedding model
    return true;
}

std::string GemmaEmbeddingModel::GetTranscription(RSState& /*state*/) {
    return "";  // Not an ASR model
}

int GemmaEmbeddingModel::GetEmbedding(RSState& state, float** out_data) {
    auto& gs = dynamic_cast<GemmaEmbState&>(state);
    if (gs.embedding.empty() || !out_data) return 0;
    *out_data = gs.embedding.data();
    return gs.emb_dim;
}

// Auto-register
static struct GemmaEmbRegistrar {
    GemmaEmbRegistrar() {
        rs_register_model_arch("gemma-embedding", []() -> std::shared_ptr<ISpeechModel> {
            return std::make_shared<GemmaEmbeddingModel>();
        });
    }
} s_gemma_emb_registrar;

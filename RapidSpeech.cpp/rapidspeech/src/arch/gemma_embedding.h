#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include <vector>
#include <string>
#include <unordered_map>

/**
 * Gemma 300M embedding model hyperparameters.
 * Used for intent recognition via semantic similarity.
 */
struct GemmaEmbHParams {
    int hidden_dim = 768;
    int n_layers = 18;
    int n_heads = 12;
    int head_dim = 64;
    int intermediate_dim = 3072;
    int vocab_size = 256000;
    int max_seq_len = 512;
    int output_dim = 768;       // MRL: can truncate to 128/256/512
    float rms_norm_eps = 1e-6f;
    float rope_theta = 10000.0f;
};

/**
 * State for Gemma embedding inference.
 */
struct GemmaEmbState : public RSState {
    std::vector<int> token_ids;
    std::vector<float> embedding;  // output embedding
    int emb_dim = 0;
};

/**
 * Gemma transformer layer weights.
 */
struct GemmaEmbLayer {
    struct ggml_tensor* attn_q_weight = nullptr;
    struct ggml_tensor* attn_k_weight = nullptr;
    struct ggml_tensor* attn_v_weight = nullptr;
    struct ggml_tensor* attn_out_weight = nullptr;
    struct ggml_tensor* attn_norm_weight = nullptr;

    struct ggml_tensor* ff_gate_weight = nullptr;
    struct ggml_tensor* ff_up_weight = nullptr;
    struct ggml_tensor* ff_down_weight = nullptr;
    struct ggml_tensor* ff_norm_weight = nullptr;
};

/**
 * All Gemma embedding model weights.
 */
struct GemmaEmbWeights {
    struct ggml_tensor* token_embedding = nullptr;
    std::vector<GemmaEmbLayer> layers;
    struct ggml_tensor* final_norm_weight = nullptr;
};

/**
 * Gemma 300M embedding model — ggml native.
 * Produces 768-dim text embeddings for semantic similarity matching.
 * Supports MRL (Matryoshka Representation Learning) truncation.
 *
 * Used by IntentRecognizer for matching utterances against trigger phrases.
 */
class GemmaEmbeddingModel : public ISpeechModel {
public:
    GemmaEmbeddingModel();
    ~GemmaEmbeddingModel() override;

    bool Load(const std::unique_ptr<rs_context_t>& ctx,
              ggml_backend_t backend) override;
    std::shared_ptr<RSState> CreateState() override;
    bool Encode(const std::vector<float>& input_frames, RSState& state,
                ggml_backend_sched_t sched) override;
    bool Decode(RSState& state, ggml_backend_sched_t sched) override;
    std::string GetTranscription(RSState& state) override;
    int GetEmbedding(RSState& state, float** out_data) override;
    const RSModelMeta& GetMeta() const override { return meta_; }

    /**
     * Encode token IDs directly (bypasses audio path).
     * Used by IntentRecognizer after tokenizing text.
     */
    bool EncodeTokens(const std::vector<int>& tokens, RSState& state,
                      ggml_backend_sched_t sched);

private:
    RSModelMeta meta_;
    GemmaEmbHParams hparams_;
    GemmaEmbWeights weights_;
    std::unordered_map<int, std::string> vocab_;

    bool MapTensors(ggml_context* gguf_data);
    bool LoadVocab(gguf_context* ctx_gguf);

    ggml_tensor* BuildTransformer(ggml_context* ctx0, ggml_tensor* token_emb,
                                   int seq_len);
};

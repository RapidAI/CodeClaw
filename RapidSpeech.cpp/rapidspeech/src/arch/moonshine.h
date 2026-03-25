#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include <vector>
#include <string>
#include <unordered_map>

/**
 * Moonshine model hyperparameters (from GGUF metadata).
 * Supports both non-streaming (tiny/base) and streaming variants.
 */
struct MoonshineHParams {
    // Encoder
    int encoder_dim = 288;       // tiny=288, base=416
    int encoder_depth = 6;       // tiny=6, base=8
    int encoder_heads = 8;
    int encoder_head_dim = 36;   // encoder_dim / encoder_heads

    // Decoder
    int decoder_dim = 288;
    int decoder_depth = 6;
    int decoder_heads = 8;
    int decoder_head_dim = 36;

    // Vocabulary
    int vocab_size = 32768;
    int bos_id = 1;
    int eos_id = 2;
    int max_seq_len = 448;

    // Audio frontend
    int sample_rate = 16000;

    // Streaming-specific
    bool is_streaming = false;
    int frame_len = 80;          // samples per frame
    int total_lookahead = 16;    // encoder lookahead frames
    int d_model_frontend = 288;
    int c1 = 576;                // conv1 output channels
    int c2 = 288;                // conv2 output channels

    // RoPE
    float rope_theta = 10000.0f;
};

/**
 * Moonshine decoder KV cache state.
 */
struct MoonshineKVCache {
    std::vector<float> k;  // [n_layers, n_heads, seq_len, head_dim]
    std::vector<float> v;
    int seq_len = 0;
};

/**
 * Runtime state for Moonshine inference.
 */
struct MoonshineState : public RSState {
    // Encoder hidden states output
    std::vector<float> encoder_out;
    int encoder_frames = 0;

    // Decoder KV caches
    MoonshineKVCache self_kv;    // self-attention cache
    MoonshineKVCache cross_kv;   // cross-attention cache (from encoder output)
    bool cross_kv_valid = false;

    // Generated token sequence
    std::vector<int> tokens;

    // Streaming state
    std::vector<float> sample_buffer;
    std::vector<float> conv1_buffer;
    std::vector<float> conv2_buffer;
    int64_t frame_count = 0;
    std::vector<float> accumulated_features;
    int accumulated_feature_count = 0;
    int encoder_frames_emitted = 0;

    // Transcription result
    std::string text_result;
};

/**
 * Moonshine encoder layer weights.
 */
struct MoonshineEncoderLayer {
    // Self-attention
    struct ggml_tensor* attn_qkv_weight = nullptr;
    struct ggml_tensor* attn_qkv_bias = nullptr;
    struct ggml_tensor* attn_out_weight = nullptr;
    struct ggml_tensor* attn_out_bias = nullptr;
    struct ggml_tensor* attn_norm_weight = nullptr;
    struct ggml_tensor* attn_norm_bias = nullptr;

    // Feed-forward
    struct ggml_tensor* ff_up_weight = nullptr;
    struct ggml_tensor* ff_up_bias = nullptr;
    struct ggml_tensor* ff_down_weight = nullptr;
    struct ggml_tensor* ff_down_bias = nullptr;
    struct ggml_tensor* ff_norm_weight = nullptr;
    struct ggml_tensor* ff_norm_bias = nullptr;
};

/**
 * Moonshine decoder layer weights.
 */
struct MoonshineDecoderLayer {
    // Self-attention
    struct ggml_tensor* self_attn_q_weight = nullptr;
    struct ggml_tensor* self_attn_k_weight = nullptr;
    struct ggml_tensor* self_attn_v_weight = nullptr;
    struct ggml_tensor* self_attn_out_weight = nullptr;
    struct ggml_tensor* self_attn_norm_weight = nullptr;
    struct ggml_tensor* self_attn_norm_bias = nullptr;

    // Cross-attention
    struct ggml_tensor* cross_attn_q_weight = nullptr;
    struct ggml_tensor* cross_attn_k_weight = nullptr;
    struct ggml_tensor* cross_attn_v_weight = nullptr;
    struct ggml_tensor* cross_attn_out_weight = nullptr;
    struct ggml_tensor* cross_attn_norm_weight = nullptr;
    struct ggml_tensor* cross_attn_norm_bias = nullptr;

    // Feed-forward
    struct ggml_tensor* ff_up_weight = nullptr;
    struct ggml_tensor* ff_up_bias = nullptr;
    struct ggml_tensor* ff_down_weight = nullptr;
    struct ggml_tensor* ff_down_bias = nullptr;
    struct ggml_tensor* ff_norm_weight = nullptr;
    struct ggml_tensor* ff_norm_bias = nullptr;
};

/**
 * All Moonshine model weights.
 */
struct MoonshineWeights {
    // Audio frontend (1D convolutions)
    struct ggml_tensor* frontend_conv1_weight = nullptr;
    struct ggml_tensor* frontend_conv1_bias = nullptr;
    struct ggml_tensor* frontend_conv2_weight = nullptr;
    struct ggml_tensor* frontend_conv2_bias = nullptr;
    struct ggml_tensor* frontend_linear_weight = nullptr;
    struct ggml_tensor* frontend_linear_bias = nullptr;

    // Encoder layers
    std::vector<MoonshineEncoderLayer> encoder_layers;
    struct ggml_tensor* encoder_final_norm_weight = nullptr;
    struct ggml_tensor* encoder_final_norm_bias = nullptr;

    // Decoder layers
    std::vector<MoonshineDecoderLayer> decoder_layers;
    struct ggml_tensor* decoder_final_norm_weight = nullptr;
    struct ggml_tensor* decoder_final_norm_bias = nullptr;

    // Token embedding + output projection
    struct ggml_tensor* token_embedding = nullptr;
    struct ggml_tensor* lm_head_weight = nullptr;
    struct ggml_tensor* lm_head_bias = nullptr;
};

/**
 * Moonshine ASR model — ggml native implementation.
 * Encoder-decoder transformer with RoPE, supporting both
 * offline and streaming modes.
 */
class MoonshineModel : public ISpeechModel {
public:
    MoonshineModel();
    ~MoonshineModel() override;

    bool Load(const std::unique_ptr<rs_context_t>& ctx,
              ggml_backend_t backend) override;
    std::shared_ptr<RSState> CreateState() override;
    bool Encode(const std::vector<float>& input_frames, RSState& state,
                ggml_backend_sched_t sched) override;
    bool Decode(RSState& state, ggml_backend_sched_t sched) override;
    std::string GetTranscription(RSState& state) override;
    const RSModelMeta& GetMeta() const override { return meta_; }

    // Streaming: push audio chunk, returns number of new encoder frames
    int PushStreamingAudio(RSState& state, const float* audio, int n_samples,
                           ggml_backend_sched_t sched);

private:
    RSModelMeta meta_;
    MoonshineHParams hparams_;
    MoonshineWeights weights_;

    // Simple BPE vocabulary (loaded from GGUF)
    std::unordered_map<int, std::string> vocab_;

    bool MapTensors(ggml_context* gguf_data);
    bool LoadVocab(gguf_context* ctx_gguf);

    // Build encoder computation graph
    ggml_tensor* BuildEncoder(ggml_context* ctx0, ggml_tensor* audio_features,
                              int n_frames);

    // Build single decoder step
    ggml_tensor* BuildDecoderStep(ggml_context* ctx0,
                                  ggml_tensor* token_emb,
                                  ggml_tensor* encoder_out,
                                  int enc_frames, int step);

    // RoPE helper
    ggml_tensor* ApplyRoPE(ggml_context* ctx0, ggml_tensor* x,
                           int head_dim, int offset);
};

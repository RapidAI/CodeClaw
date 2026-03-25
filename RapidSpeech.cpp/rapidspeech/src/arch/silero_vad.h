#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include <vector>
#include <string>

/**
 * Silero VAD hyperparameters (loaded from GGUF metadata).
 */
struct SileroVadHParams {
    int sample_rate = 16000;
    int window_size_samples = 512;   // 32ms at 16kHz
    int context_samples = 64;
    int state_size = 256;            // 2 * 1 * 128 flattened
    float threshold = 0.5f;
    int min_silence_ms = 100;
    int min_speech_ms = 250;
    int speech_pad_ms = 30;
};

/**
 * Runtime state for Silero VAD inference.
 */
struct SileroVadState : public RSState {
    // Hidden state: [2, 1, 128] flattened
    std::vector<float> h_state;
    // Context buffer: last 64 samples from previous chunk
    std::vector<float> context;
    // Last computed speech probability
    float speech_prob = 0.0f;
    // Speech detection state machine
    bool is_speaking = false;
    int silence_samples = 0;
    int speech_samples = 0;

    SileroVadState() {
        h_state.resize(256, 0.0f);
        context.resize(64, 0.0f);
    }
};

/**
 * Silero VAD weights mapped from GGUF tensors.
 * The model is a small STFT + Conv + LSTM network.
 */
struct SileroVadWeights {
    // STFT / feature extraction convolutions
    struct ggml_tensor* stft_conv_weight = nullptr;
    struct ggml_tensor* stft_conv_bias = nullptr;

    // Encoder convolution blocks
    struct ggml_tensor* enc_conv1_weight = nullptr;
    struct ggml_tensor* enc_conv1_bias = nullptr;
    struct ggml_tensor* enc_conv2_weight = nullptr;
    struct ggml_tensor* enc_conv2_bias = nullptr;

    // LSTM layers
    struct ggml_tensor* lstm_weight_ih = nullptr;
    struct ggml_tensor* lstm_weight_hh = nullptr;
    struct ggml_tensor* lstm_bias_ih = nullptr;
    struct ggml_tensor* lstm_bias_hh = nullptr;

    // Output projection
    struct ggml_tensor* out_weight = nullptr;
    struct ggml_tensor* out_bias = nullptr;
};

/**
 * Silero VAD model — ggml native implementation.
 * Implements ISpeechModel for integration with RapidSpeech framework,
 * but primarily used standalone via the VAD C API.
 */
class SileroVadModel : public ISpeechModel {
public:
    SileroVadModel();
    ~SileroVadModel() override;

    bool Load(const std::unique_ptr<rs_context_t>& ctx,
              ggml_backend_t backend) override;
    std::shared_ptr<RSState> CreateState() override;
    bool Encode(const std::vector<float>& input_frames, RSState& state,
                ggml_backend_sched_t sched) override;
    bool Decode(RSState& state, ggml_backend_sched_t sched) override;
    std::string GetTranscription(RSState& state) override;
    const RSModelMeta& GetMeta() const override { return meta_; }

    // VAD-specific: get speech probability from last Encode call
    float GetSpeechProbability(RSState& state);

private:
    RSModelMeta meta_;
    SileroVadHParams hparams_;
    SileroVadWeights weights_;

    bool MapTensors(ggml_context* gguf_data);
};

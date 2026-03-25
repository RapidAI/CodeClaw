#include "arch/silero_vad.h"
#include "core/rs_context.h"
#include "utils/rs_log.h"
#include "ggml.h"
#include "ggml-backend.h"
#include "ggml-alloc.h"
#include "gguf.h"

#include <cstring>
#include <cmath>
#include <algorithm>

// ============================================================
// Silero VAD ggml-native implementation
//
// Silero VAD v5 architecture (simplified):
//   Input PCM (576 samples = 512 window + 64 context)
//   -> STFT-like 1D convolution (feature extraction)
//   -> Encoder conv blocks with ReLU
//   -> LSTM (hidden_size=128, num_layers=2)
//   -> Linear projection -> Sigmoid -> speech probability
//
// The GGUF file stores tensors with prefix "_model."
// ============================================================

SileroVadModel::SileroVadModel() {
    meta_.arch_name = "silero-vad";
    meta_.audio_sample_rate = 16000;
    meta_.n_mels = 0;  // VAD doesn't use mel features
    meta_.vocab_size = 0;
}

SileroVadModel::~SileroVadModel() = default;

bool SileroVadModel::MapTensors(ggml_context* gguf_data) {
    // Map tensors from GGUF. Silero VAD tensors use "_model." prefix.
    // The exact tensor names depend on the PyTorch export.
    // We try common patterns from silero_vad.pt state_dict.

    auto get = [&](const char* name) -> ggml_tensor* {
        ggml_tensor* t = ggml_get_tensor(gguf_data, name);
        if (!t) {
            RS_LOG_WARN("VAD tensor not found: %s", name);
        }
        return t;
    };

    // STFT convolution
    weights_.stft_conv_weight = get("_model.stft.forward_basis_buffer");

    // Encoder blocks - adaptive to naming conventions
    weights_.enc_conv1_weight = get("_model.encoder.0.reparam_conv.weight");
    weights_.enc_conv1_bias   = get("_model.encoder.0.reparam_conv.bias");
    weights_.enc_conv2_weight = get("_model.encoder.1.reparam_conv.weight");
    weights_.enc_conv2_bias   = get("_model.encoder.1.reparam_conv.bias");

    // LSTM
    weights_.lstm_weight_ih = get("_model.decoder.rnn.weight_ih_l0");
    weights_.lstm_weight_hh = get("_model.decoder.rnn.weight_hh_l0");
    weights_.lstm_bias_ih   = get("_model.decoder.rnn.bias_ih_l0");
    weights_.lstm_bias_hh   = get("_model.decoder.rnn.bias_hh_l0");

    // Output linear
    weights_.out_weight = get("_model.decoder.decoder.2.weight");
    weights_.out_bias   = get("_model.decoder.decoder.2.bias");

    // At minimum we need the LSTM and output layers
    if (!weights_.lstm_weight_ih || !weights_.out_weight) {
        RS_LOG_ERR("Critical VAD tensors missing. Check GGUF conversion.");
        return false;
    }

    return true;
}

bool SileroVadModel::Load(const std::unique_ptr<rs_context_t>& ctx,
                          ggml_backend_t /*backend*/) {
    if (!ctx || !ctx->gguf_data) return false;

    // Read hyperparameters from GGUF metadata if available
    if (ctx->ctx_gguf) {
        int64_t key;
        key = gguf_find_key(ctx->ctx_gguf, "vad.sample_rate");
        if (key >= 0) hparams_.sample_rate = gguf_get_val_i32(ctx->ctx_gguf, key);
        key = gguf_find_key(ctx->ctx_gguf, "vad.window_size");
        if (key >= 0) hparams_.window_size_samples = gguf_get_val_i32(ctx->ctx_gguf, key);
        key = gguf_find_key(ctx->ctx_gguf, "vad.threshold");
        if (key >= 0) hparams_.threshold = gguf_get_val_f32(ctx->ctx_gguf, key);
    }

    meta_.audio_sample_rate = hparams_.sample_rate;

    if (!MapTensors(ctx->gguf_data)) {
        return false;
    }

    RS_LOG_INFO("Silero VAD loaded: sr=%d, window=%d, threshold=%.2f",
                hparams_.sample_rate, hparams_.window_size_samples,
                hparams_.threshold);
    return true;
}

std::shared_ptr<RSState> SileroVadModel::CreateState() {
    return std::make_shared<SileroVadState>();
}

// ============================================================
// LSTM cell implementation using ggml
// ============================================================

/**
 * Build a single LSTM step as a ggml computation graph.
 * input: [1, input_size]
 * h_prev, c_prev: [1, hidden_size]
 * Returns: new_h, new_c via output tensors in the graph
 */
static ggml_tensor* build_lstm_step(
    ggml_context* ctx0,
    ggml_tensor* input,
    ggml_tensor* h_prev,
    ggml_tensor* c_prev,
    ggml_tensor* w_ih,   // [4*hidden, input_size]
    ggml_tensor* w_hh,   // [4*hidden, hidden_size]
    ggml_tensor* b_ih,   // [4*hidden]
    ggml_tensor* b_hh,   // [4*hidden]
    ggml_tensor** out_c)
{
    // gates = W_ih @ input + b_ih + W_hh @ h_prev + b_hh
    ggml_tensor* ih = ggml_mul_mat(ctx0, w_ih, input);
    if (b_ih) ih = ggml_add(ctx0, ih, b_ih);

    ggml_tensor* hh = ggml_mul_mat(ctx0, w_hh, h_prev);
    if (b_hh) hh = ggml_add(ctx0, hh, b_hh);

    ggml_tensor* gates = ggml_add(ctx0, ih, hh);

    int hidden_size = (int)(ggml_nelements(h_prev));

    // Split into i, f, g, o gates
    ggml_tensor* i_gate = ggml_sigmoid(ctx0, ggml_view_1d(ctx0, gates, hidden_size, 0));
    ggml_tensor* f_gate = ggml_sigmoid(ctx0, ggml_view_1d(ctx0, gates, hidden_size,
                                        hidden_size * ggml_element_size(gates)));
    ggml_tensor* g_gate = ggml_tanh(ctx0, ggml_view_1d(ctx0, gates, hidden_size,
                                     2 * hidden_size * ggml_element_size(gates)));
    ggml_tensor* o_gate = ggml_sigmoid(ctx0, ggml_view_1d(ctx0, gates, hidden_size,
                                        3 * hidden_size * ggml_element_size(gates)));

    // c_new = f * c_prev + i * g
    ggml_tensor* c_new = ggml_add(ctx0,
        ggml_mul(ctx0, f_gate, c_prev),
        ggml_mul(ctx0, i_gate, g_gate));

    // h_new = o * tanh(c_new)
    ggml_tensor* h_new = ggml_mul(ctx0, o_gate, ggml_tanh(ctx0, c_new));

    if (out_c) *out_c = c_new;
    return h_new;
}

bool SileroVadModel::Encode(const std::vector<float>& input_frames,
                            RSState& state,
                            ggml_backend_sched_t sched) {
    auto& vad_state = dynamic_cast<SileroVadState&>(state);

    int effective_size = hparams_.window_size_samples + hparams_.context_samples;
    if ((int)input_frames.size() < effective_size) {
        RS_LOG_WARN("VAD: input too short (%zu < %d)", input_frames.size(), effective_size);
        return false;
    }

    // Build ggml computation graph
    const int n_nodes = 512;
    size_t mem_size = n_nodes * ggml_tensor_overhead() + (2 * 1024 * 1024);
    struct ggml_init_params gparams = { mem_size, nullptr, true };
    ggml_context* ctx0 = ggml_init(gparams);
    if (!ctx0) return false;

    ggml_cgraph* gf = ggml_new_graph_custom(ctx0, n_nodes, false);

    // Create input tensor from PCM data
    ggml_tensor* inp = ggml_new_tensor_1d(ctx0, GGML_TYPE_F32, effective_size);
    ggml_set_name(inp, "vad_input");
    ggml_set_input(inp);

    // Create state tensors (h and c for LSTM)
    int hidden_size = 128;
    ggml_tensor* h_in = ggml_new_tensor_1d(ctx0, GGML_TYPE_F32, hidden_size);
    ggml_set_name(h_in, "h_state_in");
    ggml_set_input(h_in);

    ggml_tensor* c_in = ggml_new_tensor_1d(ctx0, GGML_TYPE_F32, hidden_size);
    ggml_set_name(c_in, "c_state_in");
    ggml_set_input(c_in);

    // Encoder: 1D convolutions with ReLU
    ggml_tensor* x = inp;

    if (weights_.enc_conv1_weight) {
        x = ggml_conv_1d(ctx0, weights_.enc_conv1_weight, x, 1, 0, 1);
        if (weights_.enc_conv1_bias) {
            x = ggml_add(ctx0, x, weights_.enc_conv1_bias);
        }
        x = ggml_relu(ctx0, x);
    }

    if (weights_.enc_conv2_weight) {
        x = ggml_conv_1d(ctx0, weights_.enc_conv2_weight, x, 1, 0, 1);
        if (weights_.enc_conv2_bias) {
            x = ggml_add(ctx0, x, weights_.enc_conv2_bias);
        }
        x = ggml_relu(ctx0, x);
    }

    // Flatten conv output for LSTM input
    int conv_out_size = (int)ggml_nelements(x);
    x = ggml_reshape_1d(ctx0, x, conv_out_size);

    // LSTM step
    ggml_tensor* c_out = nullptr;
    ggml_tensor* h_out = build_lstm_step(ctx0, x, h_in, c_in,
        weights_.lstm_weight_ih, weights_.lstm_weight_hh,
        weights_.lstm_bias_ih, weights_.lstm_bias_hh,
        &c_out);

    // Output projection: linear + sigmoid
    ggml_tensor* logit = ggml_mul_mat(ctx0, weights_.out_weight, h_out);
    if (weights_.out_bias) {
        logit = ggml_add(ctx0, logit, weights_.out_bias);
    }
    ggml_tensor* prob = ggml_sigmoid(ctx0, logit);
    ggml_set_name(prob, "speech_prob");
    ggml_set_output(prob);

    // Mark state outputs
    ggml_set_name(h_out, "h_state_out");
    ggml_set_output(h_out);
    ggml_set_name(c_out, "c_state_out");
    ggml_set_output(c_out);

    ggml_build_forward_expand(gf, prob);
    ggml_build_forward_expand(gf, h_out);
    ggml_build_forward_expand(gf, c_out);

    // Allocate and run
    if (!ggml_backend_sched_alloc_graph(sched, gf)) {
        RS_LOG_ERR("VAD: graph allocation failed");
        ggml_free(ctx0);
        return false;
    }

    // Set input data
    ggml_backend_tensor_set(inp, input_frames.data(), 0,
                            effective_size * sizeof(float));
    ggml_backend_tensor_set(h_in, vad_state.h_state.data(), 0,
                            hidden_size * sizeof(float));
    // c_state is stored in second half of h_state vector
    ggml_backend_tensor_set(c_in, vad_state.h_state.data() + hidden_size, 0,
                            hidden_size * sizeof(float));

    if (!ggml_backend_sched_graph_compute(sched, gf)) {
        RS_LOG_ERR("VAD: graph compute failed");
        ggml_free(ctx0);
        return false;
    }

    // Read outputs
    float speech_p = 0.0f;
    ggml_backend_tensor_get(prob, &speech_p, 0, sizeof(float));
    vad_state.speech_prob = speech_p;

    // Update LSTM state
    ggml_backend_tensor_get(h_out, vad_state.h_state.data(), 0,
                            hidden_size * sizeof(float));
    ggml_backend_tensor_get(c_out, vad_state.h_state.data() + hidden_size, 0,
                            hidden_size * sizeof(float));

    // Update context buffer (last 64 samples)
    int ctx_start = effective_size - hparams_.context_samples;
    std::copy(input_frames.begin() + ctx_start, input_frames.end(),
              vad_state.context.begin());

    // State machine: speech start/end detection
    int sr_per_ms = hparams_.sample_rate / 1000;
    if (speech_p >= hparams_.threshold) {
        vad_state.speech_samples += hparams_.window_size_samples;
        vad_state.silence_samples = 0;
        if (vad_state.speech_samples >= hparams_.min_speech_ms * sr_per_ms) {
            vad_state.is_speaking = true;
        }
    } else {
        vad_state.silence_samples += hparams_.window_size_samples;
        if (vad_state.is_speaking &&
            vad_state.silence_samples >= hparams_.min_silence_ms * sr_per_ms) {
            vad_state.is_speaking = false;
            vad_state.speech_samples = 0;
        }
    }

    ggml_free(ctx0);
    return true;
}

bool SileroVadModel::Decode(RSState& /*state*/, ggml_backend_sched_t /*sched*/) {
    // VAD doesn't have a separate decode step
    return true;
}

std::string SileroVadModel::GetTranscription(RSState& state) {
    auto& vad_state = dynamic_cast<SileroVadState&>(state);
    return vad_state.is_speaking ? "SPEECH" : "SILENCE";
}

float SileroVadModel::GetSpeechProbability(RSState& state) {
    return dynamic_cast<SileroVadState&>(state).speech_prob;
}

// Auto-register architecture
static struct SileroVadRegistrar {
    SileroVadRegistrar() {
        rs_register_model_arch("silero-vad", []() -> std::shared_ptr<ISpeechModel> {
            return std::make_shared<SileroVadModel>();
        });
    }
} s_silero_vad_registrar;

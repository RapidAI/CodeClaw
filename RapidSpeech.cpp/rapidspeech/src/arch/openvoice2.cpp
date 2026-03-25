#include "openvoice2.h"
#include "core/rs_context.h"
#include "ggml-backend.h"
#include "ggml.h"
#include "gguf.h"
#include "utils/rs_log.h"

#include <algorithm>
#include <cmath>
#include <cstring>
#include <functional>
#include <numeric>

#define OV2_MAX_NODES 4096

// =====================================================================
// OpenVoice2Model implementation
// =====================================================================

OpenVoice2Model::OpenVoice2Model() {}
OpenVoice2Model::~OpenVoice2Model() {}

bool OpenVoice2Model::MapTensors(std::map<std::string, struct ggml_tensor*>& all) {
  for (auto& [name, tensor] : all) {
    if (name.find("text_encoder.") == 0) {
      weights_.text_encoder[name] = tensor;
    } else if (name.find("duration_predictor.") == 0) {
      weights_.duration_predictor[name] = tensor;
    } else if (name.find("flow_decoder.") == 0) {
      weights_.flow_decoder[name] = tensor;
    } else if (name.find("vocoder.") == 0) {
      weights_.vocoder[name] = tensor;
    } else if (name.find("posterior_encoder.") == 0) {
      weights_.posterior_encoder[name] = tensor;
    } else if (name.find("emb_") == 0) {
      weights_.embeddings[name] = tensor;
    } else {
      // Store in text_encoder as fallback (some weights may not have prefix)
      weights_.text_encoder[name] = tensor;
    }
  }
  RS_LOG_INFO("OpenVoice2: mapped %zu text_enc, %zu dur_pred, %zu flow, %zu vocoder tensors",
              weights_.text_encoder.size(), weights_.duration_predictor.size(),
              weights_.flow_decoder.size(), weights_.vocoder.size());
  return true;
}

bool OpenVoice2Model::Load(const std::unique_ptr<rs_context_t>& ctx,
                            ggml_backend_t backend) {
  if (!ctx || !ctx->ctx_gguf || !ctx->gguf_data) {
    RS_LOG_ERR("Invalid context for OpenVoice2 Load");
    return false;
  }

  gguf_context* ctx_gguf = ctx->ctx_gguf;
  ggml_context* gguf_data = ctx->gguf_data;

  // Load hyperparameters from GGUF KV
  int64_t key;
  key = gguf_find_key(ctx_gguf, "openvoice2.hidden_channels");
  if (key != -1) hparams_.hidden_channels = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "openvoice2.sample_rate");
  if (key != -1) hparams_.sample_rate = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "openvoice2.hop_length");
  if (key != -1) hparams_.hop_length = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "openvoice2.n_fft");
  if (key != -1) hparams_.n_fft = gguf_get_val_i32(ctx_gguf, key);

  meta_.arch_name = "openvoice2";
  meta_.audio_sample_rate = hparams_.sample_rate;
  meta_.n_mels = hparams_.n_mels;
  meta_.vocab_size = hparams_.vocab_size;

  RS_LOG_INFO("OpenVoice2: hidden=%d, sr=%d, hop=%d",
              hparams_.hidden_channels, hparams_.sample_rate, hparams_.hop_length);

  // Init text frontend
  text_frontend_.Init(nullptr);

  // Map all tensors
  std::map<std::string, struct ggml_tensor*> tensors;
  const int n_tensors = gguf_get_n_tensors(ctx_gguf);
  for (int i = 0; i < n_tensors; ++i) {
    const char* name = gguf_get_tensor_name(ctx_gguf, i);
    struct ggml_tensor* t = ggml_get_tensor(gguf_data, name);
    if (t) tensors[name] = t;
  }

  return MapTensors(tensors);
}

bool OpenVoice2Model::LoadConverter(const char* converter_path,
                                     ggml_backend_t backend) {
  // TODO: Load tone color converter GGUF
  // For now, voice cloning is not yet supported
  RS_LOG_INFO("OpenVoice2: converter loading not yet implemented: %s", converter_path);
  converter_weights_.loaded = false;
  return true;  // Non-fatal: base TTS works without converter
}

std::shared_ptr<RSState> OpenVoice2Model::CreateState() {
  return std::make_shared<OpenVoice2State>();
}

// =====================================================================
// TTS-specific methods
// =====================================================================

bool OpenVoice2Model::PushText(RSState& state, const char* text,
                                const char* language) {
  auto& s = static_cast<OpenVoice2State&>(state);
  s.language = language ? language : "zh";
  s.phoneme_ids = text_frontend_.TextToPhonemeIds(text, s.language);

  if (s.phoneme_ids.empty()) {
    RS_LOG_ERR("OpenVoice2: text frontend produced no phonemes");
    return false;
  }

  RS_LOG_INFO("OpenVoice2: text -> %zu phoneme IDs", s.phoneme_ids.size());
  return true;
}

bool OpenVoice2Model::PushReferenceAudio(RSState& state, const float* samples,
                                          int n_samples, int sample_rate,
                                          ggml_backend_sched_t sched) {
  auto& s = static_cast<OpenVoice2State&>(state);

  if (!converter_weights_.loaded) {
    RS_LOG_WARN("OpenVoice2: tone converter not loaded, ignoring reference audio");
    return true;  // Non-fatal
  }

  // TODO: compute mel spectrogram from reference audio
  // TODO: run tone color encoder to extract style embedding
  // For now, placeholder
  s.has_tone_embedding = false;
  return true;
}

// =====================================================================
// Encode: TextEncoder + DurationPredictor + FlowDecoder
// =====================================================================

bool OpenVoice2Model::Encode(const std::vector<float>& input_frames,
                              RSState& state, ggml_backend_sched_t sched) {
  auto& s = static_cast<OpenVoice2State&>(state);
  (void)input_frames;  // TTS doesn't use audio input for encoding

  if (s.phoneme_ids.empty()) {
    RS_LOG_ERR("OpenVoice2: no text pushed, call PushText first");
    return false;
  }

  // Step 1: Text Encoder
  if (!RunTextEncoder(s, sched)) return false;

  // Step 2: Duration Predictor
  if (!RunDurationPredictor(s, sched)) return false;

  // Step 3: Flow Decoder (generates full mel spectrogram)
  if (!RunFlowDecoder(s, sched)) return false;

  // Reset streaming cursor
  s.mel_chunk_cursor = 0;
  s.audio_output.clear();
  s.audio_read_cursor = 0;

  return true;
}

// =====================================================================
// Decode: Vocoder on next mel chunk (streaming)
// =====================================================================

bool OpenVoice2Model::Decode(RSState& state, ggml_backend_sched_t sched) {
  auto& s = static_cast<OpenVoice2State&>(state);

  if (s.mel_spectrogram.empty() || s.mel_chunk_cursor >= s.total_mel_frames) {
    return false;  // No more chunks
  }

  int chunk_size = hparams_.chunk_mel_frames;
  if (chunk_size <= 0) chunk_size = s.total_mel_frames;  // Non-streaming

  int mel_start = s.mel_chunk_cursor;
  int mel_len = std::min(chunk_size, s.total_mel_frames - mel_start);

  if (!RunVocoder(s, sched, mel_start, mel_len)) return false;

  s.mel_chunk_cursor += mel_len;
  return true;
}

int OpenVoice2Model::GetAudioOutput(RSState& state, float** out_data) {
  auto& s = static_cast<OpenVoice2State&>(state);
  if (s.audio_read_cursor >= static_cast<int>(s.audio_output.size())) {
    return 0;
  }
  *out_data = s.audio_output.data() + s.audio_read_cursor;
  int n = static_cast<int>(s.audio_output.size()) - s.audio_read_cursor;
  s.audio_read_cursor = static_cast<int>(s.audio_output.size());
  return n;
}

// =====================================================================
// Sub-graph implementations (skeleton — real ggml graphs TBD)
// =====================================================================

bool OpenVoice2Model::RunTextEncoder(OpenVoice2State& state,
                                      ggml_backend_sched_t sched) {
  // Text Encoder: phoneme IDs → hidden states
  // Architecture: Embedding → Transformer layers → output
  //
  // For now: placeholder that creates dummy hidden states
  // Real implementation will build ggml graph from weights_.text_encoder

  int T = static_cast<int>(state.phoneme_ids.size());
  int C = hparams_.hidden_channels;

  state.encoder_hidden.resize(C * T, 0.0f);
  state.encoder_T = T;

  // Placeholder: initialize with small random-ish values based on phoneme IDs
  for (int t = 0; t < T; t++) {
    float base = static_cast<float>(state.phoneme_ids[t]) / 256.0f;
    for (int c = 0; c < C; c++) {
      state.encoder_hidden[c * T + t] = base * sinf(static_cast<float>(c) * 0.1f);
    }
  }

  RS_LOG_INFO("OpenVoice2: TextEncoder -> [%d, %d]", C, T);
  return true;
}

bool OpenVoice2Model::RunDurationPredictor(OpenVoice2State& state,
                                            ggml_backend_sched_t sched) {
  // Duration Predictor: encoder hidden → duration per phoneme
  // Output: integer durations (mel frames per phoneme)
  //
  // Placeholder: assign ~5 mel frames per phoneme

  int T = state.encoder_T;
  state.durations.resize(T);
  state.total_mel_frames = 0;

  for (int t = 0; t < T; t++) {
    // Heuristic: vowels get more frames, consonants fewer
    int dur = 5;  // default
    state.durations[t] = dur;
    state.total_mel_frames += dur;
  }

  RS_LOG_INFO("OpenVoice2: DurationPredictor -> %d total mel frames", state.total_mel_frames);
  return true;
}

bool OpenVoice2Model::RunFlowDecoder(OpenVoice2State& state,
                                      ggml_backend_sched_t sched) {
  // Flow Decoder: expand encoder hidden by durations → mel spectrogram
  // Architecture: normalizing flow layers
  //
  // Placeholder: generate dummy mel spectrogram

  int n_mels = hparams_.n_mels;
  int T_mel = state.total_mel_frames;

  state.mel_spectrogram.resize(n_mels * T_mel, 0.0f);

  // Expand encoder hidden states according to durations
  int mel_pos = 0;
  for (int t = 0; t < state.encoder_T && mel_pos < T_mel; t++) {
    int dur = state.durations[t];
    for (int d = 0; d < dur && mel_pos < T_mel; d++) {
      for (int m = 0; m < n_mels; m++) {
        // Simple interpolation from encoder hidden
        int c_idx = m % hparams_.hidden_channels;
        float val = state.encoder_hidden[c_idx * state.encoder_T + t];
        state.mel_spectrogram[m * T_mel + mel_pos] = val;
      }
      mel_pos++;
    }
  }

  RS_LOG_INFO("OpenVoice2: FlowDecoder -> mel [%d, %d]", n_mels, T_mel);
  return true;
}

bool OpenVoice2Model::RunVocoder(OpenVoice2State& state,
                                  ggml_backend_sched_t sched,
                                  int mel_start, int mel_len) {
  // HiFi-GAN Vocoder: mel chunk → audio waveform
  //
  // Placeholder: generate silence with correct length
  // Real implementation will build ggml graph from weights_.vocoder

  int samples_per_frame = hparams_.hop_length;
  int n_samples = mel_len * samples_per_frame;

  // Append to audio output buffer
  size_t prev_size = state.audio_output.size();
  state.audio_output.resize(prev_size + n_samples, 0.0f);

  // Placeholder: generate a simple waveform from mel values
  int n_mels = hparams_.n_mels;
  int T_mel = state.total_mel_frames;

  for (int i = 0; i < n_samples; i++) {
    int mel_frame = mel_start + i / samples_per_frame;
    if (mel_frame >= T_mel) break;

    // Sum a few mel bands to create a rough waveform
    float sum = 0.0f;
    for (int m = 0; m < std::min(8, n_mels); m++) {
      sum += state.mel_spectrogram[m * T_mel + mel_frame];
    }
    float t = static_cast<float>(i % samples_per_frame) / samples_per_frame;
    state.audio_output[prev_size + i] = sum * sinf(2.0f * 3.14159f * 440.0f * t) * 0.01f;
  }

  RS_LOG_INFO("OpenVoice2: Vocoder chunk [%d..%d] -> %d samples",
              mel_start, mel_start + mel_len, n_samples);
  return true;
}

bool OpenVoice2Model::RunToneColorEncoder(OpenVoice2State& state,
                                           const std::vector<float>& mel,
                                           ggml_backend_sched_t sched) {
  // Tone Color Encoder: reference mel → style embedding
  // TODO: implement when converter weights are loaded
  state.has_tone_embedding = false;
  return true;
}

// =====================================================================
// Static registration
// =====================================================================
namespace {
struct OpenVoice2Registrar {
  OpenVoice2Registrar() {
    rs_register_model_arch("openvoice2", []() {
      return std::make_shared<OpenVoice2Model>();
    });
  }
} global_openvoice2_reg;
}  // namespace

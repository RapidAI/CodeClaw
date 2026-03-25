#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include "frontend/text_frontend.h"
#include <map>
#include <string>
#include <vector>

// =====================================================================
// OpenVoice2 TTS Model (MeloTTS/VITS base + Tone Color Converter)
//
// Architecture:
//   Text → TextFrontend → PhonemeIDs → TextEncoder → DurationPredictor
//     → FlowDecoder (chunked) → HiFi-GAN Vocoder → Audio
//
// Optional voice cloning:
//   RefAudio → ToneColorEncoder → StyleEmbedding → inject into decoder
//
// Streaming: FlowDecoder outputs mel chunks, vocoder converts per-chunk.
// =====================================================================

struct OpenVoice2HParams {
  int32_t hidden_channels = 192;
  int32_t inter_channels  = 192;
  int32_t filter_channels = 768;
  int32_t n_heads         = 2;
  int32_t n_layers        = 6;
  int32_t n_flow_layers   = 4;
  int32_t vocab_size      = 256;
  int32_t sample_rate     = 22050;
  int32_t hop_length      = 256;
  int32_t n_fft           = 1024;
  int32_t n_mels          = 80;
  // Streaming chunk size in mel frames (0 = non-streaming)
  int32_t chunk_mel_frames = 50;  // ~0.58s per chunk at 22050/256
};

// --- OpenVoice2 State ---
struct OpenVoice2State : public RSState {
  // Text frontend output
  std::vector<int32_t> phoneme_ids;
  std::string language;

  // Encoder output (hidden states for all phonemes)
  std::vector<float> encoder_hidden;  // [hidden_channels, T_text]
  int encoder_T = 0;

  // Duration predictor output → mel frame count per phoneme
  std::vector<int32_t> durations;
  int total_mel_frames = 0;

  // Flow decoder output (full mel spectrogram, generated incrementally)
  std::vector<float> mel_spectrogram;  // [n_mels, total_mel_frames]

  // Streaming state
  int mel_chunk_cursor = 0;  // next mel frame to vocoder

  // Audio output buffer (accumulated from vocoder chunks)
  std::vector<float> audio_output;
  int audio_read_cursor = 0;

  // Tone color embedding (optional, from reference audio)
  std::vector<float> tone_embedding;
  bool has_tone_embedding = false;

  OpenVoice2State() {}
  ~OpenVoice2State() override = default;
};

// --- OpenVoice2 Weight Structures ---

struct OpenVoice2Weights {
  // Text Encoder (Transformer)
  std::map<std::string, struct ggml_tensor*> text_encoder;

  // Duration Predictor
  std::map<std::string, struct ggml_tensor*> duration_predictor;

  // Flow Decoder
  std::map<std::string, struct ggml_tensor*> flow_decoder;

  // HiFi-GAN Vocoder
  std::map<std::string, struct ggml_tensor*> vocoder;

  // Posterior Encoder (needed for VITS training path, optional at inference)
  std::map<std::string, struct ggml_tensor*> posterior_encoder;

  // Embedding tables
  std::map<std::string, struct ggml_tensor*> embeddings;
};

// --- Tone Color Converter Weights (separate GGUF) ---
struct ToneConverterWeights {
  std::map<std::string, struct ggml_tensor*> all_tensors;
  bool loaded = false;
};

// =====================================================================
// OpenVoice2Model class
// =====================================================================

class OpenVoice2Model : public ISpeechModel {
public:
  OpenVoice2Model();
  ~OpenVoice2Model() override;

  // ISpeechModel interface
  bool Load(const std::unique_ptr<rs_context_t>& ctx, ggml_backend_t backend) override;
  std::shared_ptr<RSState> CreateState() override;

  // Encode: for TTS, this runs TextEncoder + DurationPredictor + FlowDecoder
  // input_frames is unused for TTS (text is pushed via PushText)
  bool Encode(const std::vector<float>& input_frames, RSState& state,
              ggml_backend_sched_t sched) override;

  // Decode: runs vocoder on next mel chunk, returns 1 if audio available, 0 if done
  bool Decode(RSState& state, ggml_backend_sched_t sched) override;

  std::string GetTranscription(RSState& state) override { (void)state; return ""; }
  const RSModelMeta& GetMeta() const override { return meta_; }

  // --- TTS-specific methods ---

  /// Push text for synthesis. Must be called before Encode.
  bool PushText(RSState& state, const char* text, const char* language = "zh");

  /// Push reference audio for voice cloning (optional).
  bool PushReferenceAudio(RSState& state, const float* samples, int n_samples,
                          int sample_rate, ggml_backend_sched_t sched);

  /// Load tone color converter from a separate GGUF file.
  bool LoadConverter(const char* converter_path, ggml_backend_t backend);

  /// Get audio output chunk. Returns number of samples, 0 if no more.
  int GetAudioOutput(RSState& state, float** out_data);

private:
  RSModelMeta meta_;
  OpenVoice2HParams hparams_;
  OpenVoice2Weights weights_;
  ToneConverterWeights converter_weights_;
  TextFrontend text_frontend_;

  bool MapTensors(std::map<std::string, struct ggml_tensor*>& all_tensors);

  // Sub-graph builders
  bool RunTextEncoder(OpenVoice2State& state, ggml_backend_sched_t sched);
  bool RunDurationPredictor(OpenVoice2State& state, ggml_backend_sched_t sched);
  bool RunFlowDecoder(OpenVoice2State& state, ggml_backend_sched_t sched);
  bool RunVocoder(OpenVoice2State& state, ggml_backend_sched_t sched,
                  int mel_start, int mel_len);
  bool RunToneColorEncoder(OpenVoice2State& state, const std::vector<float>& mel,
                           ggml_backend_sched_t sched);
};

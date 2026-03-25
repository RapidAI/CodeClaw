#pragma once

#include <vector>
#include <cstdint>
#include <string>

// Configuration for SenseVoice/FunASR frontend pipeline
enum FbankMode {
  FBANK_KALDI = 0,       // Kaldi-style: pre-emphasis, DC removal, Slaney mel, ln()
  FBANK_SPEECHBRAIN = 1, // SpeechBrain-style: center padding, HTK mel, 10*log10, mean norm
};

struct STFTConfig {
  int sample_rate = 16000;
  int frame_size = 400; // 25ms @ 16k
  int frame_step = 160; // 10ms @ 16k
  int n_fft = 512;      // Power of 2 padding (Kaldi mode)
  int n_mels = 80;
  float f_min = 31.748642f;
  float f_max = 8000.0f;

  FbankMode fbank_mode = FBANK_KALDI;

  // --- SpeechBrain mode specific ---
  bool use_sentence_norm = false;  // per-utterance mean subtraction
  float sb_top_db = 80.0f;        // amplitude_to_DB top_db clipping

  // --- SenseVoice Specific (LFR & CMVN) ---
  bool use_lfr = true;
  int lfr_m = 7;        // Stack 7 frames
  int lfr_n = 6;        // Stride 6 frames

  bool use_cmvn = true;
  int fbank_num_threads = 2;
  // Default CMVN values are usually provided via weights/file
};

struct CMVNData {
  std::vector<float> means;
  std::vector<float> vars;
};

class AudioProcessor {
public:
  AudioProcessor(const STFTConfig& config);
  ~AudioProcessor();

  // Set CMVN parameters (extracted from model weights or external file)
  void SetCMVN(const std::vector<float>& means, const std::vector<float>& vars);

  // Main pipeline: PCM -> Fbank -> LFR -> CMVN
  void Compute(const std::vector<float>& input_pcm,
               std::vector<float>& output_feats);

private:
  STFTConfig config_;
  std::vector<double> hamming_window_;
  std::vector<float> mel_filters_; // [n_mels, n_fft/2 + 1]
  CMVNData cmvn_;

  // Internal FFT workspace
  std::vector<int> fft_ip_;
  std::vector<double> fft_w_;

  // Precomputed DFT twiddle factors for SpeechBrain mode (non-power-of-2 n_fft)
  // dft_cos_[k * n_fft + n] = cos(-2*pi*k*n/n_fft)
  // dft_sin_[k * n_fft + n] = sin(-2*pi*k*n/n_fft)
  std::vector<float> dft_cos_;
  std::vector<float> dft_sin_;
  float log10_amin_ = 0.0f;  // precomputed 10*log10(amin) for amplitude_to_DB

  void InitTables();
  void InitMelFilters();
  void InitMelFiltersSpeechBrain();
  void InitDFTTable();

  // Core processing steps
  void ComputeFbank(const std::vector<float>& samples, std::vector<float>& output_mel);
  void ComputeFbankSpeechBrain(const std::vector<float>& samples, std::vector<float>& output_mel);
  void ApplyLFR(const std::vector<float>& input_mel, int n_frames, std::vector<float>& output_lfr);
  void ApplyCMVN(std::vector<float>& feats);
  void ProcessFrame(int i, const std::vector<float>& samples,
                                        std::vector<double>& window,
                                        std::vector<double>& power_spec,
                                        std::vector<float>& output_mel);
  void ProcessFrameSpeechBrain(int i, const std::vector<float>& samples_padded,
                                int n_padded,
                                std::vector<double>& window,
                                std::vector<double>& power_spec,
                                std::vector<float>& output_mel);

  // Mathematical utilities
  int RoundToPowerOfTwo(int n);
};
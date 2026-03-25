#include "frontend/audio_processor.h"
#include <cmath>
#include <algorithm>
#include <iostream>
#include <cstring>
#include <cassert>
#include <vector>
#include <thread>


// Check if OpenMP is available
#if defined(_OPENMP)
#include <omp.h>
#endif

#ifndef M_PI
#define M_PI 3.14159265358979323846
#endif

#define PREEMPH_COEFF 0.97f

// External reference to rdft
extern "C" {
    void rdft(int n, int isgn, double *a, int *ip, double *w);
}

static inline double mel_scale(double hz) {
    return 1127.0 * std::log(1.0 + hz / 700.0);
}

AudioProcessor::AudioProcessor(const STFTConfig& config) : config_(config) {
    if (config_.fbank_mode == FBANK_KALDI) {
        config_.n_fft = RoundToPowerOfTwo(config_.frame_size);
    }
    InitTables();
    if (config_.fbank_mode == FBANK_SPEECHBRAIN) {
        InitMelFiltersSpeechBrain();
        InitDFTTable();
    } else {
        InitMelFilters();
    }
}

AudioProcessor::~AudioProcessor() {}

int AudioProcessor::RoundToPowerOfTwo(int n) {
    n--;
    n |= n >> 1; n |= n >> 2; n |= n >> 4; n |= n >> 8; n |= n >> 16;
    return n + 1;
}

void AudioProcessor::InitTables() {
    // 1. Hamming Window
    hamming_window_.resize(config_.frame_size);
    for (int i = 0; i < config_.frame_size; i++) {
        hamming_window_[i] = 0.54 - 0.46 * cos((2.0 * M_PI * i) / (config_.frame_size));
    }

    // 2. FFT Tables (Ooura rdft — only for Kaldi mode, requires power-of-2 n_fft)
    if (config_.fbank_mode != FBANK_SPEECHBRAIN) {
        fft_ip_.assign(2 + static_cast<int>(sqrt(config_.n_fft / 2)) + 1, 0);
        fft_w_.assign(config_.n_fft / 2, 0.0);
        fft_ip_[0] = 0;

        // Dry run to generate internal tables (makes ip/w read-only and thread-safe)
        std::vector<double> dummy(config_.n_fft, 0.0);
        rdft(config_.n_fft, 1, dummy.data(), fft_ip_.data(), fft_w_.data());
    }
}

void AudioProcessor::InitMelFilters() {
    const int n_mel = config_.n_mels;
    const int fft_bins = config_.n_fft / 2;
    const int num_bins = fft_bins;

    mel_filters_.assign(n_mel * num_bins, 0.0f);

    const double mel_low_freq = 31.748642;
    const double mel_freq_delta = 34.6702385;
    const double fft_bin_width = (double)config_.sample_rate / config_.n_fft;

    for (int i = 0; i < n_mel; i++) {
        double left_mel   = mel_low_freq + i * mel_freq_delta;
        double center_mel = mel_low_freq + (i + 1.0) * mel_freq_delta;
        double right_mel  = mel_low_freq + (i + 2.0) * mel_freq_delta;

        for (int j = 0; j < num_bins; j++) {
            double freq_hz = fft_bin_width * j;
            double mel_num = mel_scale(freq_hz);

            double up_slope   = (mel_num - left_mel) / (center_mel - left_mel);
            double down_slope = (right_mel - mel_num) / (right_mel - center_mel);

            double filter_val = std::max(0.0, std::min(up_slope, down_slope));
            mel_filters_[i * num_bins + j] = static_cast<float>(filter_val);
        }
    }
}

// SpeechBrain-style mel filterbank initialization
// Uses HTK mel scale: mel = 2595 * log10(1 + hz/700)
// Triangular filters with f_min=0, f_max=sample_rate/2
// Filter matrix is [n_stft, n_mels] where n_stft = n_fft/2 + 1
void AudioProcessor::InitMelFiltersSpeechBrain() {
    const int n_mel = config_.n_mels;
    const int n_stft = config_.n_fft / 2 + 1;
    const double f_min = 0.0;
    const double f_max = config_.sample_rate / 2.0;

    mel_filters_.assign(n_mel * n_stft, 0.0f);

    // HTK mel scale
    auto hz_to_mel = [](double hz) -> double {
        return 2595.0 * std::log10(1.0 + hz / 700.0);
    };
    auto mel_to_hz = [](double mel) -> double {
        return 700.0 * (std::pow(10.0, mel / 2595.0) - 1.0);
    };

    // n_mels+2 points linearly spaced in mel domain
    std::vector<double> mel_points(n_mel + 2);
    double mel_min = hz_to_mel(f_min);
    double mel_max = hz_to_mel(f_max);
    for (int i = 0; i < n_mel + 2; i++) {
        mel_points[i] = mel_min + (mel_max - mel_min) * i / (n_mel + 1);
    }
    std::vector<double> hz_points(n_mel + 2);
    for (int i = 0; i < n_mel + 2; i++) {
        hz_points[i] = mel_to_hz(mel_points[i]);
    }

    // Central frequencies and bands
    // band[i] = hz_points[i+1] - hz_points[i], use band[0..n_mel-1]
    std::vector<double> f_central(n_mel);
    std::vector<double> band(n_mel);
    for (int i = 0; i < n_mel; i++) {
        f_central[i] = hz_points[i + 1];
        band[i] = hz_points[i + 1] - hz_points[i];
    }

    // All frequency bins: [0, sr/2] with n_stft points
    std::vector<double> all_freqs(n_stft);
    for (int j = 0; j < n_stft; j++) {
        all_freqs[j] = (double)(config_.sample_rate / 2) * j / (n_stft - 1);
    }

    // Triangular filters: slope = (freq - center) / band
    // left_side = slope + 1, right_side = -slope + 1
    // filter = max(0, min(left_side, right_side))
    // Store as [n_mel, n_stft] (same layout as Kaldi filters for ProcessFrame compat)
    for (int i = 0; i < n_mel; i++) {
        for (int j = 0; j < n_stft; j++) {
            double slope = (all_freqs[j] - f_central[i]) / band[i];
            double left_side = slope + 1.0;
            double right_side = -slope + 1.0;
            double val = std::max(0.0, std::min(left_side, right_side));
            mel_filters_[i * n_stft + j] = static_cast<float>(val);
        }
    }
}

// Precompute DFT twiddle factors for non-power-of-2 n_fft (SpeechBrain mode).
// This avoids repeated cos/sin calls in ProcessFrameSpeechBrain.
void AudioProcessor::InitDFTTable() {
    int N = config_.n_fft;
    int n_stft = N / 2 + 1;
    dft_cos_.resize(n_stft * N);
    dft_sin_.resize(n_stft * N);
    for (int k = 0; k < n_stft; k++) {
        double phase_step = -2.0 * M_PI * k / N;
        for (int n = 0; n < N; n++) {
            double angle = phase_step * n;
            dft_cos_[k * N + n] = static_cast<float>(std::cos(angle));
            dft_sin_[k * N + n] = static_cast<float>(std::sin(angle));
        }
    }
    // Precompute constant for amplitude_to_DB
    log10_amin_ = 10.0f * std::log10(1e-14);
}

// Core processing unit: Process a single frame.
// Note: window_buf and power_spec_buf are provided by the caller to avoid repeated allocation.
void AudioProcessor::ProcessFrame(int frame_idx,
                                  const std::vector<float>& samples,
                                  std::vector<double>& window_buf,
                                  std::vector<double>& power_spec_buf,
                                  std::vector<float>& output_mel) {

    int n_samples = samples.size();
    int offset = frame_idx * config_.frame_step;

    // 1. Copy and Pad
    // Use std::fill for zeroing, often faster/cleaner than loop assignment
    std::fill(window_buf.begin(), window_buf.end(), 0.0);

    int copy_len = std::min(config_.frame_size, n_samples - offset);
    if (copy_len > 0) {
        for (int j = 0; j < copy_len; j++) {
            window_buf[j] = static_cast<double>(samples[offset + j]);
        }
    }

    // 2. Remove DC
    double sum = 0.0;
    for (int j = 0; j < config_.frame_size; j++) sum += window_buf[j];
    double mean = sum / config_.frame_size;
    for (int j = 0; j < config_.frame_size; j++) window_buf[j] -= mean;

    // 3. Pre-emphasis
    for (int j = config_.frame_size - 1; j > 0; j--) {
        window_buf[j] -= PREEMPH_COEFF * window_buf[j - 1];
    }
    window_buf[0] -= PREEMPH_COEFF * window_buf[0];

    // 4. Hamming window
    for (int j = 0; j < config_.frame_size; j++) {
        window_buf[j] *= hamming_window_[j];
    }

    // 5. FFT
    // Note: Since ip/w arrays are pre-initialized, they are read-only and thread-safe.
    rdft(config_.n_fft, 1, window_buf.data(), const_cast<int*>(fft_ip_.data()), const_cast<double*>(fft_w_.data()));

    // 6. Power Spectrum (Corrected logic)
    // Ooura rdft layout:
    // a[0] = Re(0) (DC)
    // a[1] = Re(N/2) (Nyquist) - This is a separate real number, not the imaginary part of DC
    // a[2k] = Re(k), a[2k+1] = Im(k)

    power_spec_buf[0] = window_buf[0] * window_buf[0];           // DC
    power_spec_buf[config_.n_fft / 2] = window_buf[1] * window_buf[1]; // Nyquist

    for (int j = 1; j < config_.n_fft / 2; j++) {
        power_spec_buf[j] = window_buf[2 * j] * window_buf[2 * j] +
                            window_buf[2 * j + 1] * window_buf[2 * j + 1];
    }

    // 7. Mel Filtering
    // Leverage cache locality
    int num_bins = config_.n_fft / 2;
    for (int j = 0; j < config_.n_mels; j++) {
        double mel_energy = 0.0;
        const float* filter_row = &mel_filters_[j * num_bins];

        // Core dot product loop, usually auto-vectorized by compiler (SIMD)
        for (int k = 0; k < num_bins; k++) {
             mel_energy += power_spec_buf[k] * filter_row[k];
        }

        output_mel[frame_idx * config_.n_mels + j] = static_cast<float>(log(std::max(mel_energy, 1.19e-7)));
    }
}

void AudioProcessor::ComputeFbank(const std::vector<float>& samples, std::vector<float>& output_mel) {
    int n_samples = samples.size();
    if (n_samples < config_.frame_size) return;

    int n_frames = (n_samples - config_.frame_size) / config_.frame_step + 1;
    output_mel.resize(n_frames * config_.n_mels);

    // ==========================================
    // Strategy 1: OpenMP Parallelization
    // ==========================================
#if defined(_OPENMP)
    #pragma omp parallel
    {
        // Thread-private buffers to avoid repeated allocation
        std::vector<double> window_buf(config_.n_fft);
        std::vector<double> power_spec_buf(config_.n_fft / 2 + 1);

        #pragma omp for
        for (int i = 0; i < n_frames; i++) {
            ProcessFrame(i, samples, window_buf, power_spec_buf, output_mel);
        }
    }

    // ==========================================
    // Strategy 2: std::thread Parallelization (Fallback)
    // ==========================================
#else
    unsigned int num_threads = config_.fbank_num_threads;
    if (num_threads == 0) num_threads = 1; // Safety fallback

    // If there are too few frames, threading overhead outweighs benefits; use single thread.
    if (n_frames < num_threads * 2) num_threads = 1;

    std::vector<std::thread> threads;
    int chunk_size = n_frames / num_threads;

    for (unsigned int t = 0; t < num_threads; t++) {
        int start = t * chunk_size;
        int end = (t == num_threads - 1) ? n_frames : (t + 1) * chunk_size;

        threads.emplace_back([this, start, end, &samples, &output_mel]() {
            // Thread-local buffer
            std::vector<double> window_buf(config_.n_fft);
            std::vector<double> power_spec_buf(config_.n_fft / 2 + 1);

            for (int i = start; i < end; i++) {
                ProcessFrame(i, samples, window_buf, power_spec_buf, output_mel);
            }
        });
    }

    for (auto& t : threads) {
        if (t.joinable()) t.join();
    }
#endif
}

// SpeechBrain-style frame processing: no pre-emphasis, no DC removal
// Just window + FFT + power spectrum + mel filtering with log10 scale
// NOTE: Uses direct DFT (not Ooura rdft) because SpeechBrain n_fft=400
// is not a power of 2, and Ooura rdft requires power-of-2 lengths.
void AudioProcessor::ProcessFrameSpeechBrain(int frame_idx,
                                              const std::vector<float>& samples_padded,
                                              int n_padded,
                                              std::vector<double>& window_buf,
                                              std::vector<double>& power_spec_buf,
                                              std::vector<float>& output_mel) {
    int offset = frame_idx * config_.frame_step;
    int n_stft = config_.n_fft / 2 + 1;

    std::fill(window_buf.begin(), window_buf.end(), 0.0);
    int copy_len = std::min(config_.n_fft, n_padded - offset);
    if (copy_len > 0) {
        for (int j = 0; j < copy_len; j++) {
            window_buf[j] = static_cast<double>(samples_padded[offset + j]);
        }
    }

    // Hamming window (no pre-emphasis, no DC removal)
    for (int j = 0; j < config_.frame_size; j++) {
        window_buf[j] *= hamming_window_[j];
    }

    // Direct DFT using precomputed twiddle factors (bins 0..n_fft/2)
    // X[k] = sum_{n=0}^{N-1} x[n] * exp(-j*2*pi*k*n/N)
    // power_spec[k] = |X[k]|^2
    int N = config_.n_fft;
    for (int k = 0; k < n_stft; k++) {
        double re = 0.0, im = 0.0;
        const float* cos_row = &dft_cos_[k * N];
        const float* sin_row = &dft_sin_[k * N];
        for (int n = 0; n < N; n++) {
            re += window_buf[n] * cos_row[n];
            im += window_buf[n] * sin_row[n];
        }
        power_spec_buf[k] = re * re + im * im;
    }

    // Mel filtering + log10 scale (SpeechBrain amplitude_to_DB)
    const double amin = 1e-14;
    for (int m = 0; m < config_.n_mels; m++) {
        double mel_energy = 0.0;
        const float* filter_row = &mel_filters_[m * n_stft];
        for (int k = 0; k < n_stft; k++) {
            mel_energy += power_spec_buf[k] * filter_row[k];
        }
        double db = 10.0 * std::log10(std::max(mel_energy, amin)) - log10_amin_;
        output_mel[frame_idx * config_.n_mels + m] = static_cast<float>(db);
    }
}

// SpeechBrain-style fbank: center padding, no pre-emphasis, HTK mel, log10, mean norm
void AudioProcessor::ComputeFbankSpeechBrain(const std::vector<float>& samples,
                                              std::vector<float>& output_mel) {
    int n_samples = static_cast<int>(samples.size());
    if (n_samples == 0) return;

    // Center padding: pad n_fft/2 zeros on each side
    int pad_len = config_.n_fft / 2;
    std::vector<float> padded(n_samples + 2 * pad_len, 0.0f);
    std::copy(samples.begin(), samples.end(), padded.begin() + pad_len);
    int n_padded = static_cast<int>(padded.size());

    int n_frames = 1 + (n_padded - config_.n_fft) / config_.frame_step;
    if (n_frames <= 0) return;
    output_mel.resize(n_frames * config_.n_mels);

#if defined(_OPENMP)
    #pragma omp parallel
    {
        std::vector<double> window_buf(config_.n_fft);
        std::vector<double> power_spec_buf(config_.n_fft / 2 + 1);
        #pragma omp for
        for (int i = 0; i < n_frames; i++) {
            ProcessFrameSpeechBrain(i, padded, n_padded, window_buf, power_spec_buf, output_mel);
        }
    }
#else
    unsigned int num_threads = config_.fbank_num_threads;
    if (num_threads == 0) num_threads = 1;
    if (n_frames < (int)(num_threads * 2)) num_threads = 1;

    std::vector<std::thread> threads;
    int chunk_size = n_frames / num_threads;
    for (unsigned int t = 0; t < num_threads; t++) {
        int start = t * chunk_size;
        int end = (t == num_threads - 1) ? n_frames : (t + 1) * chunk_size;
        threads.emplace_back([this, start, end, &padded, n_padded, &output_mel]() {
            std::vector<double> window_buf(config_.n_fft);
            std::vector<double> power_spec_buf(config_.n_fft / 2 + 1);
            for (int i = start; i < end; i++) {
                ProcessFrameSpeechBrain(i, padded, n_padded, window_buf, power_spec_buf, output_mel);
            }
        });
    }
    for (auto& th : threads) {
        if (th.joinable()) th.join();
    }
#endif

    // top_db clipping
    float max_val = -1e30f;
    for (size_t i = 0; i < output_mel.size(); i++) {
        if (output_mel[i] > max_val) max_val = output_mel[i];
    }
    float clip_val = max_val - config_.sb_top_db;
    for (size_t i = 0; i < output_mel.size(); i++) {
        if (output_mel[i] < clip_val) output_mel[i] = clip_val;
    }

    // Sentence-level mean normalization (per feature dim)
    if (config_.use_sentence_norm) {
        std::vector<double> mean_acc(config_.n_mels, 0.0);
        for (int i = 0; i < n_frames; i++) {
            for (int j = 0; j < config_.n_mels; j++) {
                mean_acc[j] += output_mel[i * config_.n_mels + j];
            }
        }
        for (int j = 0; j < config_.n_mels; j++) {
            float m = static_cast<float>(mean_acc[j] / n_frames);
            for (int i = 0; i < n_frames; i++) {
                output_mel[i * config_.n_mels + j] -= m;
            }
        }
    }
}

void AudioProcessor::ApplyLFR(const std::vector<float>& input_mel, int n_frames, std::vector<float>& output_lfr) {
    int m = config_.lfr_m;
    int n = config_.lfr_n;
    int n_mels = config_.n_mels;

    int T_lfr = static_cast<int>(ceil(1.0 * n_frames / n));
    output_lfr.resize(T_lfr * m * n_mels);

    int left_pad = (m - 1) / 2;

    // LFR can also be parallelized; however, as it's memory-bound (copying),
    // the acceleration depends heavily on memory bandwidth.
    #if defined(_OPENMP)
    #pragma omp parallel for
    #endif
    for (int i = 0; i < T_lfr; i++) {
        for (int j = 0; j < m; j++) {
            int source_frame_idx = i * n - left_pad + j;
            if (source_frame_idx < 0) source_frame_idx = 0;
            if (source_frame_idx >= n_frames) source_frame_idx = n_frames - 1;

            std::memcpy(output_lfr.data() + (i * m * n_mels) + (j * n_mels),
                        input_mel.data() + (source_frame_idx * n_mels),
                        n_mels * sizeof(float));
        }
    }
}

void AudioProcessor::SetCMVN(const std::vector<float>& means, const std::vector<float>& vars) {
    cmvn_.means = means;
    cmvn_.vars = vars;
}

void AudioProcessor::ApplyCMVN(std::vector<float>& feats) {
    if (cmvn_.means.empty() || cmvn_.vars.empty()) return;

    int feat_dim = config_.lfr_m * config_.n_mels;
    int n_frames = feats.size() / feat_dim;

    #if defined(_OPENMP)
    #pragma omp parallel for
    #endif
    for (int i = 0; i < n_frames; i++) {
        for (int j = 0; j < feat_dim; j++) {
            int idx = i * feat_dim + j;
            feats[idx] = (feats[idx] + cmvn_.means[j]) * cmvn_.vars[j];
        }
    }
}

void AudioProcessor::Compute(const std::vector<float>& input_pcm, std::vector<float>& output_feats) {
    if (input_pcm.empty()) return;

    std::vector<float> mel_feats;
    if (config_.fbank_mode == FBANK_SPEECHBRAIN) {
        ComputeFbankSpeechBrain(input_pcm, mel_feats);
    } else {
        ComputeFbank(input_pcm, mel_feats);
    }
    int n_frames = mel_feats.size() / config_.n_mels;

    if (config_.use_lfr) {
        std::vector<float> lfr_feats;
        ApplyLFR(mel_feats, n_frames, lfr_feats);
        output_feats = std::move(lfr_feats);
    } else {
        output_feats = std::move(mel_feats);
    }

    if (config_.use_cmvn) {
        ApplyCMVN(output_feats);
    }
}
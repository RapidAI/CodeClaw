#include "rapidspeech.h"
#include "utils/rs_log.h"
#include "utils/rs_wav.h"

#include <cstring>
#include <iostream>
#include <string>
#include <vector>

static void print_usage(const char *prog) {
  std::cerr
      << "Usage:\n"
      << "  " << prog << " --model <ecapa.gguf> --wav1 <a.wav> --wav2 <b.wav> [options]\n\n"
      << "Options:\n"
      << "  -m, --model <path>       Speaker model file (GGUF, required)\n"
      << "  --wav1 <path>            First WAV file (required)\n"
      << "  --wav2 <path>            Second WAV file (required)\n"
      << "  --threshold <float>      Similarity threshold (default: 0.5)\n"
      << "  -t, --threads <num>      Number of threads (default: 4)\n"
      << "      --gpu <true|false>   Use GPU (default: true)\n"
      << std::endl;
}

static bool parse_bool(const std::string &v) {
  return (v == "1" || v == "true" || v == "TRUE");
}

int main(int argc, char *argv[]) {
  if (argc < 2) {
    print_usage(argv[0]);
    return 1;
  }

  const char *model_path = nullptr;
  const char *wav1_path  = nullptr;
  const char *wav2_path  = nullptr;
  float threshold        = 0.5f;
  int n_threads          = 4;
  bool use_gpu           = true;

  for (int i = 1; i < argc; ++i) {
    std::string arg = argv[i];
    if ((arg == "-m" || arg == "--model") && i + 1 < argc) {
      model_path = argv[++i];
    } else if (arg == "--wav1" && i + 1 < argc) {
      wav1_path = argv[++i];
    } else if (arg == "--wav2" && i + 1 < argc) {
      wav2_path = argv[++i];
    } else if (arg == "--threshold" && i + 1 < argc) {
      threshold = std::stof(argv[++i]);
    } else if ((arg == "-t" || arg == "--threads") && i + 1 < argc) {
      n_threads = std::stoi(argv[++i]);
    } else if (arg == "--gpu" && i + 1 < argc) {
      use_gpu = parse_bool(argv[++i]);
    } else {
      std::cerr << "Unknown argument: " << arg << std::endl;
      print_usage(argv[0]);
      return 1;
    }
  }

  if (!model_path || !wav1_path || !wav2_path) {
    std::cerr << "Error: --model, --wav1, and --wav2 are all required\n";
    print_usage(argv[0]);
    return 1;
  }

  // Init speaker embedding context
  rs_init_params_t params = rs_default_params();
  params.model_path = model_path;
  params.n_threads  = n_threads;
  params.use_gpu    = use_gpu;
  params.task_type  = RS_TASK_SPEAKER_EMBED;

  std::cout << "[speaker-verify] Model     : " << model_path << std::endl;
  std::cout << "[speaker-verify] WAV1      : " << wav1_path << std::endl;
  std::cout << "[speaker-verify] WAV2      : " << wav2_path << std::endl;
  std::cout << "[speaker-verify] Threshold : " << threshold << std::endl;

  rs_context_t *ctx = rs_init_from_file(params);
  if (!ctx) {
    std::cerr << "[speaker-verify] Failed to initialize context." << std::endl;
    return 1;
  }

  // Load WAV files
  std::vector<float> pcm1, pcm2;
  int sr1 = 16000, sr2 = 16000;

  if (!load_wav_file(wav1_path, pcm1, &sr1)) {
    std::cerr << "[speaker-verify] Failed to load WAV1: " << wav1_path << std::endl;
    rs_free(ctx);
    return 1;
  }
  if (!load_wav_file(wav2_path, pcm2, &sr2)) {
    std::cerr << "[speaker-verify] Failed to load WAV2: " << wav2_path << std::endl;
    rs_free(ctx);
    return 1;
  }

  std::cout << "[speaker-verify] WAV1: " << pcm1.size() << " samples @ " << sr1 << " Hz" << std::endl;
  std::cout << "[speaker-verify] WAV2: " << pcm2.size() << " samples @ " << sr2 << " Hz" << std::endl;

  // Compute similarity
  float score = rs_speaker_verify(ctx,
                                  pcm1.data(), static_cast<int>(pcm1.size()),
                                  pcm2.data(), static_cast<int>(pcm2.size()),
                                  16000);

  if (score <= -2.0f) {
    std::cerr << "[speaker-verify] Verification failed (inference error)." << std::endl;
    rs_free(ctx);
    return 1;
  }

  bool same = score >= threshold;
  std::cout << "[speaker-verify] Score        : " << score << std::endl;
  std::cout << "[speaker-verify] Same speaker : " << (same ? "YES" : "NO") << std::endl;

  rs_free(ctx);
  return 0;
}

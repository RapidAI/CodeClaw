#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include <map>
#include <string>
#include <vector>

// --- ECAPA-TDNN Hyperparameters ---
struct EcapaTdnnHParams {
  int32_t n_mels        = 80;
  int32_t channels      = 1024;   // C=1024 for ECAPA-TDNN large
  int32_t emb_dim       = 192;    // output embedding dimension
  int32_t n_se_res2_blocks = 3;   // number of SE-Res2Block layers
  int32_t res2_scale    = 8;      // Res2Block internal scale (number of sub-bands)
  int32_t attn_channels = 128;    // ASP attention bottleneck channels
  // Kernel sizes for the 3 SE-Res2Blocks
  int32_t kernel_sizes[3] = {3, 3, 3};
  // Dilation sizes for the 3 SE-Res2Blocks
  int32_t dilations[3]    = {2, 3, 4};
  float   eps           = 1e-5f;
};

// --- ECAPA-TDNN State ---
struct EcapaTdnnState : public RSState {
  // Output embedding buffer (192-dim)
  std::vector<float> embedding;
  // Debug: dump directory (empty = no dump)
  std::string debug_dump_dir;

  EcapaTdnnState() {}
  ~EcapaTdnnState() override = default;
};

// --- Weight structures ---

// A single 1D convolution block: Conv1d + BatchNorm + ReLU
struct TDNNBlock {
  struct ggml_tensor* conv_w;   // [out_ch, in_ch, kernel]
  struct ggml_tensor* conv_b;
  struct ggml_tensor* bn_w;     // [out_ch]
  struct ggml_tensor* bn_b;
  struct ggml_tensor* bn_mean;
  struct ggml_tensor* bn_var;
};

// SE (Squeeze-and-Excitation) block weights
struct SEBlock {
  struct ggml_tensor* fc1_w;    // [channels/se_r, channels]
  struct ggml_tensor* fc1_b;
  struct ggml_tensor* fc2_w;    // [channels, channels/se_r]
  struct ggml_tensor* fc2_b;
};

// SE-Res2Block: the core building block of ECAPA-TDNN
// Conv1d(1x1) -> split into sub-bands -> Conv1d per sub-band -> concat -> Conv1d(1x1) -> SE -> residual
struct SERes2Block {
  TDNNBlock tdnn1;
  std::vector<TDNNBlock> res2_convs;  // size = res2_scale - 1
  TDNNBlock tdnn2;
  SEBlock se;
  // Shortcut: bare Conv1d (no BN) when in_channels != out_channels
  struct ggml_tensor* shortcut_w = nullptr;  // [out_ch, in_ch, 1]
  struct ggml_tensor* shortcut_b = nullptr;
  bool has_shortcut = false;
};

// Full ECAPA-TDNN model weights (matches SpeechBrain spkrec-ecapa-voxceleb)
struct EcapaTdnnWeights {
  // Initial TDNN layer: Conv1d(n_mels, C, 5) + BN + ReLU
  TDNNBlock layer0;
  // 3 SE-Res2Blocks
  std::vector<SERes2Block> se_res2_blocks;  // size = 3
  // MFA: TDNNBlock(3*C, 3*C, 1)
  TDNNBlock mfa_conv;
  // ASP (Attentive Statistical Pooling) with global_context=true
  // tdnn: TDNNBlock(C*3, attn_ch, 1) — input is cat(x, mean, std)
  TDNNBlock asp_tdnn;
  // conv: Conv1d(attn_ch, C, 1) — channel-dependent attention
  struct ggml_tensor* asp_conv_w;  // [C, attn_ch, 1]
  struct ggml_tensor* asp_conv_b;
  // asp_bn: BatchNorm1d(C*2) — applied after pooling (mean+std concat)
  struct ggml_tensor* asp_bn_w;
  struct ggml_tensor* asp_bn_b;
  struct ggml_tensor* asp_bn_mean;
  struct ggml_tensor* asp_bn_var;
  // Final FC: Conv1d(C*2, emb_dim, 1)
  struct ggml_tensor* fc_w;  // [emb_dim, C*2, 1]
  struct ggml_tensor* fc_b;
};

// --- ECAPA-TDNN Model Class ---
class EcapaTdnnModel : public ISpeechModel {
public:
  EcapaTdnnModel();
  ~EcapaTdnnModel() override;

  bool Load(const std::unique_ptr<rs_context_t>& ctx, ggml_backend_t backend) override;
  std::shared_ptr<RSState> CreateState() override;
  bool Encode(const std::vector<float>& input_frames, RSState& state, ggml_backend_sched_t sched) override;
  bool Decode(RSState& state, ggml_backend_sched_t sched) override;
  std::string GetTranscription(RSState& state) override;
  int GetEmbedding(RSState& state, float** out_data) override;
  const RSModelMeta& GetMeta() const override { return meta_; }

private:
  RSModelMeta meta_;
  EcapaTdnnHParams hparams_;
  EcapaTdnnWeights weights_;

  bool MapTensors(std::map<std::string, struct ggml_tensor*>& tensors);
  void MapTDNNBlock(TDNNBlock& block, std::map<std::string, struct ggml_tensor*>& t,
                    const std::string& prefix);
  void MapSERes2Block(SERes2Block& block, std::map<std::string, struct ggml_tensor*>& t,
                      const std::string& prefix, bool has_shortcut);
};

#include "ecapa_tdnn.h"
#include "core/rs_context.h"
#include "ggml-backend.h"
#include "ggml-cpu.h"
#include "ggml.h"
#include "utils/rs_log.h"
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <functional>
#include <numeric>

// Max graph nodes for ECAPA-TDNN
// ECAPA-TDNN with res2_scale=8 creates many intermediate tensors:
// - Each batch_norm_1d: ~10 tensors (reshape, repeat x4, sub, add1, sqrt, div, mul, add)
// - Each SE-Res2Block: tdnn1 + 7 sub-band convs + tdnn2 + SE + residual
// - 3 blocks + layer0 + MFA + ASP with global context
// Conservative estimate: ~8000+ tensors needed
#define ECAPA_MAX_NODES 16384

// =====================================================================
// ggml graph helpers
// =====================================================================
// All tensors use ggml convention: [T, C] (ne[0]=time, ne[1]=channels)
// ggml_conv_1d expects:
//   kernel a: [K, IC, OC]  (ne[0]=kernel, ne[1]=in_ch, ne[2]=out_ch)
//   input  b: [T, IC]      (ne[0]=time, ne[1]=in_ch)
//   output:   [T', OC]     (ne[0]=out_time, ne[1]=out_ch)

static struct ggml_tensor* ggml_scalar_input_f32(struct ggml_context* ctx, const char* name) {
  struct ggml_tensor* t = ggml_new_tensor_1d(ctx, GGML_TYPE_F32, 1);
  ggml_set_name(t, name);
  ggml_set_input(t);
  return t;
}

// BatchNorm1d: y = (x - mean) / sqrt(var + eps) * w + b
// x: [T, C], params: [C] — need to reshape params to [1, C] and repeat to [T, C]
// eps_tensor: a scalar input tensor (value set after graph allocation)
static struct ggml_tensor* batch_norm_1d(struct ggml_context* ctx,
                                          struct ggml_tensor* x,
                                          struct ggml_tensor* w,
                                          struct ggml_tensor* b,
                                          struct ggml_tensor* mean,
                                          struct ggml_tensor* var,
                                          struct ggml_tensor* eps_tensor) {
  int T = x->ne[0];
  int C = x->ne[1];

  struct ggml_tensor* mean_r = ggml_repeat(ctx, ggml_reshape_2d(ctx, mean, 1, C), x);
  struct ggml_tensor* var_r  = ggml_repeat(ctx, ggml_reshape_2d(ctx, var, 1, C), x);
  struct ggml_tensor* w_r    = ggml_repeat(ctx, ggml_reshape_2d(ctx, w, 1, C), x);
  struct ggml_tensor* b_r    = ggml_repeat(ctx, ggml_reshape_2d(ctx, b, 1, C), x);

  struct ggml_tensor* cur = ggml_sub(ctx, x, mean_r);
  struct ggml_tensor* var_eps = ggml_add1(ctx, var_r, eps_tensor);
  struct ggml_tensor* inv_std = ggml_sqrt(ctx, var_eps);
  cur = ggml_div(ctx, cur, inv_std);
  cur = ggml_mul(ctx, cur, w_r);
  cur = ggml_add(ctx, cur, b_r);
  return cur;
}

// Helper: cast tensor to F16 if needed (ggml_conv_1d im2col requires F16 kernel)
static struct ggml_tensor* ensure_f16(struct ggml_context* ctx, struct ggml_tensor* t) {
  if (t->type == GGML_TYPE_F16) return t;
  return ggml_cast(ctx, t, GGML_TYPE_F16);
}

// Add bias [C] to tensor [T, C]
static struct ggml_tensor* add_bias(struct ggml_context* ctx,
                                     struct ggml_tensor* x,
                                     struct ggml_tensor* bias) {
  int C = x->ne[1];
  struct ggml_tensor* b2 = ggml_repeat(ctx, ggml_reshape_2d(ctx, bias, 1, C), x);
  return ggml_add(ctx, x, b2);
}

// TDNNBlock forward: Conv1d + BN + ReLU
// input: [T, IC] or [T, IC, 1], output: [T', OC]
static struct ggml_tensor* tdnn_block_forward(struct ggml_context* ctx,
                                               struct ggml_tensor* x,
                                               const TDNNBlock& block,
                                               int stride, int dilation,
                                               struct ggml_tensor* eps_tensor) {
  int kernel_size = block.conv_w->ne[0];
  int padding = ((kernel_size - 1) * dilation) / 2;
  struct ggml_tensor* cur = ggml_conv_1d(ctx, ensure_f16(ctx, block.conv_w), x, stride, padding, dilation);
  // conv_1d returns [OL, OC, N] (3D), squeeze to [OL, OC] (2D)
  int OL = cur->ne[0];
  int OC = cur->ne[1];
  cur = ggml_reshape_2d(ctx, cur, OL, OC);
  cur = add_bias(ctx, cur, block.conv_b);
  cur = batch_norm_1d(ctx, cur, block.bn_w, block.bn_b, block.bn_mean, block.bn_var, eps_tensor);
  cur = ggml_relu(ctx, cur);
  return cur;
}

// Debug: track a tensor for later dumping
struct DebugTensor {
  std::string name;
  struct ggml_tensor* tensor;
};

static void debug_track(std::vector<DebugTensor>& dbg, const std::string& name,
                         struct ggml_tensor* t) {
  ggml_set_name(t, name.c_str());
  ggml_set_output(t);
  dbg.push_back({name, t});
}

// Dump a tracked tensor to a raw float32 binary file (row-major)
// Also writes a companion .shape file with dimensions
static void debug_dump_tensor(const DebugTensor& dt, ggml_backend_sched_t sched,
                               const std::string& dir) {
  struct ggml_tensor* t = dt.tensor;
  int64_t n_elem = ggml_nelements(t);
  std::vector<float> buf(n_elem);
  ggml_backend_tensor_get(t, buf.data(), 0, n_elem * sizeof(float));

  // Write raw float32 data
  std::string path = dir + "/" + dt.name + ".bin";
  std::ofstream f(path, std::ios::binary);
  f.write(reinterpret_cast<const char*>(buf.data()), n_elem * sizeof(float));
  f.close();

  // Write shape info
  std::string spath = dir + "/" + dt.name + ".shape";
  std::ofstream sf(spath);
  sf << t->ne[0];
  for (int d = 1; d < GGML_MAX_DIMS && t->ne[d] > 1; d++) {
    sf << "," << t->ne[d];
  }
  sf << std::endl;
  sf.close();

  float mean = 0, mn = buf[0], mx = buf[0];
  double sum = 0;
  for (int64_t i = 0; i < n_elem; i++) {
    sum += buf[i];
    if (buf[i] < mn) mn = buf[i];
    if (buf[i] > mx) mx = buf[i];
  }
  mean = (float)(sum / n_elem);
  RS_LOG_INFO("  DEBUG %s: ne=[%lld,%lld,%lld,%lld] mean=%.6f min=%.6f max=%.6f",
              dt.name.c_str(), (long long)t->ne[0], (long long)t->ne[1],
              (long long)t->ne[2], (long long)t->ne[3], mean, mn, mx);
}

// SE-Res2Block forward
// input: [T, C], output: [T, C]
static struct ggml_tensor* se_res2_block_forward(struct ggml_context* ctx,
                                                  struct ggml_tensor* x,
                                                  const SERes2Block& block,
                                                  int res2_scale,
                                                  int kernel_size,
                                                  int dilation,
                                                  struct ggml_tensor* eps_tensor) {
  int T = x->ne[0];
  int C = x->ne[1];

  // 1. First 1x1 conv
  struct ggml_tensor* cur = tdnn_block_forward(ctx, x, block.tdnn1, 1, 1, eps_tensor);
  int C_inner = cur->ne[1];

  // 2. Res2Net multi-scale processing
  int sub_ch = C_inner / res2_scale;
  std::vector<struct ggml_tensor*> sub_outputs;
  struct ggml_tensor* prev = nullptr;

  for (int s = 0; s < res2_scale; s++) {
    // Extract sub-band: [T, sub_ch] at channel offset s*sub_ch
    // In ggml [T, C] layout: ne[0]=T, ne[1]=C, stride nb[1]=T*sizeof(float)
    struct ggml_tensor* sub = ggml_view_2d(ctx, cur, T, sub_ch,
                                            cur->nb[1], s * sub_ch * cur->nb[1]);
    if (s == 0) {
      sub_outputs.push_back(sub);
    } else {
      // Res2Net: s=1 uses sub directly; s>=2 adds previous output
      // (matches PyTorch: if i==1: inp=chunks[i], else: inp=chunks[i]+y[-1])
      if (s >= 2 && prev) {
        sub = ggml_add(ctx, sub, prev);
      }
      int conv_idx = s - 1;
      int pad = ((kernel_size - 1) * dilation) / 2;
      struct ggml_tensor* conv_out = ggml_conv_1d(ctx, ensure_f16(ctx, block.res2_convs[conv_idx].conv_w),
                                                    sub, 1, pad, dilation);
      // Squeeze 3D -> 2D
      conv_out = ggml_reshape_2d(ctx, conv_out, conv_out->ne[0], conv_out->ne[1]);
      conv_out = add_bias(ctx, conv_out, block.res2_convs[conv_idx].conv_b);
      conv_out = batch_norm_1d(ctx, conv_out,
                                block.res2_convs[conv_idx].bn_w,
                                block.res2_convs[conv_idx].bn_b,
                                block.res2_convs[conv_idx].bn_mean,
                                block.res2_convs[conv_idx].bn_var, eps_tensor);
      conv_out = ggml_relu(ctx, conv_out);
      sub_outputs.push_back(conv_out);
    }
    prev = sub_outputs.back();
  }

  // Concatenate sub-bands along channel dim (dim=1 in [T, C])
  struct ggml_tensor* cat = sub_outputs[0];
  for (int s = 1; s < res2_scale; s++) {
    cat = ggml_concat(ctx, cat, sub_outputs[s], 1);
  }

  // 3. Second 1x1 conv
  cur = tdnn_block_forward(ctx, cat, block.tdnn2, 1, 1, eps_tensor);

  // 4. SE (Squeeze-and-Excitation)
  // Global avg pool over time: [T, C] -> [1, C]
  int T_cur = cur->ne[0];
  struct ggml_tensor* se_in = ggml_pool_1d(ctx, cur, GGML_OP_POOL_AVG, T_cur, T_cur, 0);
  // FC1 via conv_1d: kernel [1, C, se_ch], input [1, C] -> [1, se_ch]
  struct ggml_tensor* se = ggml_conv_1d(ctx, ensure_f16(ctx, block.se.fc1_w), se_in, 1, 0, 1);
  se = ggml_reshape_2d(ctx, se, se->ne[0], se->ne[1]);
  se = add_bias(ctx, se, block.se.fc1_b);
  se = ggml_relu(ctx, se);
  // FC2 via conv_1d: kernel [1, se_ch, C], input [1, se_ch] -> [1, C]
  se = ggml_conv_1d(ctx, ensure_f16(ctx, block.se.fc2_w), se, 1, 0, 1);
  se = ggml_reshape_2d(ctx, se, se->ne[0], se->ne[1]);
  se = add_bias(ctx, se, block.se.fc2_b);
  se = ggml_sigmoid(ctx, se);
  // Repeat se [1, C] -> [T, C] for element-wise mul
  se = ggml_repeat(ctx, se, cur);
  cur = ggml_mul(ctx, cur, se);

  // 5. Residual connection
  struct ggml_tensor* residual = x;
  if (block.has_shortcut) {
    residual = ggml_conv_1d(ctx, ensure_f16(ctx, block.shortcut_w), x, 1, 0, 1);
    residual = ggml_reshape_2d(ctx, residual, residual->ne[0], residual->ne[1]);
    residual = add_bias(ctx, residual, block.shortcut_b);
  }
  cur = ggml_add(ctx, cur, residual);

  return cur;
}

// =====================================================================
// EcapaTdnnModel implementation
// =====================================================================

EcapaTdnnModel::EcapaTdnnModel() {}
EcapaTdnnModel::~EcapaTdnnModel() {}

void EcapaTdnnModel::MapTDNNBlock(TDNNBlock& block,
                                   std::map<std::string, struct ggml_tensor*>& t,
                                   const std::string& prefix) {
  block.conv_w  = t.at(prefix + ".conv.weight");
  block.conv_b  = t.at(prefix + ".conv.bias");
  block.bn_w    = t.at(prefix + ".norm.weight");
  block.bn_b    = t.at(prefix + ".norm.bias");
  block.bn_mean = t.at(prefix + ".norm.running_mean");
  block.bn_var  = t.at(prefix + ".norm.running_var");
}

void EcapaTdnnModel::MapSERes2Block(SERes2Block& block,
                                     std::map<std::string, struct ggml_tensor*>& t,
                                     const std::string& prefix,
                                     bool has_shortcut) {
  MapTDNNBlock(block.tdnn1, t, prefix + ".tdnn1");
  int n_res2 = hparams_.res2_scale - 1;
  block.res2_convs.resize(n_res2);
  for (int i = 0; i < n_res2; i++) {
    MapTDNNBlock(block.res2_convs[i], t, prefix + ".res2_convs." + std::to_string(i));
  }
  MapTDNNBlock(block.tdnn2, t, prefix + ".tdnn2");
  block.se.fc1_w = t.at(prefix + ".se.conv1.weight");
  block.se.fc1_b = t.at(prefix + ".se.conv1.bias");
  block.se.fc2_w = t.at(prefix + ".se.conv2.weight");
  block.se.fc2_b = t.at(prefix + ".se.conv2.bias");
  block.has_shortcut = has_shortcut;
  if (has_shortcut) {
    block.shortcut_w = t.at(prefix + ".shortcut.conv.weight");
    block.shortcut_b = t.at(prefix + ".shortcut.conv.bias");
  }
}

bool EcapaTdnnModel::MapTensors(std::map<std::string, struct ggml_tensor*>& tensors) {
  try {
    MapTDNNBlock(weights_.layer0, tensors, "blocks.0");
    weights_.se_res2_blocks.resize(hparams_.n_se_res2_blocks);
    for (int i = 0; i < hparams_.n_se_res2_blocks; i++) {
      std::string prefix = "blocks." + std::to_string(i + 1);
      bool has_shortcut = tensors.count(prefix + ".shortcut.conv.weight") > 0;
      MapSERes2Block(weights_.se_res2_blocks[i], tensors, prefix, has_shortcut);
    }
    MapTDNNBlock(weights_.mfa_conv, tensors, "mfa");
    MapTDNNBlock(weights_.asp_tdnn, tensors, "asp.tdnn");
    weights_.asp_conv_w = tensors.at("asp.conv.weight");
    weights_.asp_conv_b = tensors.at("asp.conv.bias");
    weights_.asp_bn_w    = tensors.at("asp_bn.weight");
    weights_.asp_bn_b    = tensors.at("asp_bn.bias");
    weights_.asp_bn_mean = tensors.at("asp_bn.running_mean");
    weights_.asp_bn_var  = tensors.at("asp_bn.running_var");
    weights_.fc_w = tensors.at("fc.weight");
    weights_.fc_b = tensors.at("fc.bias");
    return true;
  } catch (const std::exception& e) {
    RS_LOG_ERR("ECAPA-TDNN tensor mapping failed: %s", e.what());
    return false;
  }
}

bool EcapaTdnnModel::Load(const std::unique_ptr<rs_context_t>& ctx, ggml_backend_t backend) {
  if (!ctx || !ctx->ctx_gguf || !ctx->gguf_data) {
    RS_LOG_ERR("Invalid context for ECAPA-TDNN Load");
    return false;
  }
  gguf_context* ctx_gguf = ctx->ctx_gguf;
  ggml_context* gguf_data = ctx->gguf_data;

  int64_t key;
  key = gguf_find_key(ctx_gguf, "ecapa.channels");
  if (key != -1) hparams_.channels = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "ecapa.emb_dim");
  if (key != -1) hparams_.emb_dim = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "ecapa.n_mels");
  if (key != -1) hparams_.n_mels = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "ecapa.res2_scale");
  if (key != -1) hparams_.res2_scale = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "ecapa.attn_channels");
  if (key != -1) hparams_.attn_channels = gguf_get_val_i32(ctx_gguf, key);

  meta_.arch_name = "ecapa-tdnn";
  meta_.audio_sample_rate = 16000;
  meta_.n_mels = hparams_.n_mels;
  meta_.vocab_size = 0;

  RS_LOG_INFO("ECAPA-TDNN: C=%d, emb=%d, mels=%d, scale=%d, attn=%d",
              hparams_.channels, hparams_.emb_dim, hparams_.n_mels,
              hparams_.res2_scale, hparams_.attn_channels);

  std::map<std::string, struct ggml_tensor*> tensors;
  const int n_tensors = gguf_get_n_tensors(ctx_gguf);
  for (int i = 0; i < n_tensors; ++i) {
    const char* name = gguf_get_tensor_name(ctx_gguf, i);
    struct ggml_tensor* t = ggml_get_tensor(gguf_data, name);
    if (t) tensors[name] = t;
  }
  return MapTensors(tensors);
}

std::shared_ptr<RSState> EcapaTdnnModel::CreateState() {
  return std::make_shared<EcapaTdnnState>();
}

bool EcapaTdnnModel::Encode(const std::vector<float>& input_frames,
                             RSState& state, ggml_backend_sched_t sched) {
  auto& spk_state = static_cast<EcapaTdnnState&>(state);
  float eps = hparams_.eps;
  int C = hparams_.channels;
  int n_mels = hparams_.n_mels;
  int emb_dim = hparams_.emb_dim;

  // Debug dump setup: check state field or ECAPA_DEBUG_DUMP env var
  if (spk_state.debug_dump_dir.empty()) {
    const char* env_dump = std::getenv("ECAPA_DEBUG_DUMP");
    if (env_dump && env_dump[0]) spk_state.debug_dump_dir = env_dump;
  }
  const bool debug = !spk_state.debug_dump_dir.empty();
  std::vector<DebugTensor> dbg;

  // Debug: optionally inject fbank from file (for reproducible comparison with PyTorch)
  std::vector<float> injected_frames;
  const std::vector<float>* frames_ptr = &input_frames;
  if (debug) {
    std::string inject_path = spk_state.debug_dump_dir + "/fbank_inject.bin";
    std::ifstream inj(inject_path, std::ios::binary | std::ios::ate);
    if (inj.is_open()) {
      size_t sz = inj.tellg();
      inj.seekg(0);
      injected_frames.resize(sz / sizeof(float));
      inj.read(reinterpret_cast<char*>(injected_frames.data()), sz);
      inj.close();
      frames_ptr = &injected_frames;
      RS_LOG_INFO("DEBUG: injected fbank from %s (%zu floats)", inject_path.c_str(), injected_frames.size());
    }
  }

  // input_frames: [n_mels * T] flattened row-major [n_mels, T] (from processor)
  int T = (int)frames_ptr->size() / n_mels;
  if (T <= 0) {
    RS_LOG_ERR("ECAPA-TDNN: empty input");
    return false;
  }

  struct ggml_context* ctx0 = nullptr;
  struct ggml_cgraph* gf = nullptr;
  if (!init_compute_ctx(&ctx0, &gf, ECAPA_MAX_NODES)) return false;

  // Create scalar input tensors for eps constants (cannot use ggml_set_f32 in no_alloc context)
  struct ggml_tensor* eps_tensor = ggml_scalar_input_f32(ctx0, "bn_eps");
  struct ggml_tensor* small_eps_tensor = ggml_scalar_input_f32(ctx0, "small_eps");

  // Input: AudioProcessor gives [T * n_mels] flattened as [T, n_mels] row-major
  // (T groups of n_mels values — frame 0's n_mels features first, then frame 1, etc.)
  //
  // ggml 2D tensor ne[0]=T, ne[1]=n_mels has memory layout:
  //   nb[0]=sizeof(float), nb[1]=T*sizeof(float)
  //   element [t, mel] at offset t*4 + mel*T*4
  //   flat: n_mels groups of T values
  //
  // AudioProcessor output is T groups of n_mels values — we need to transpose.
  // Create as ne[0]=n_mels, ne[1]=T (matching AudioProcessor layout), then transpose.
  struct ggml_tensor* input_raw = ggml_new_tensor_2d(ctx0, GGML_TYPE_F32, n_mels, T);
  ggml_set_name(input_raw, "fbank_input");
  ggml_set_input(input_raw);
  // Transpose to ne[0]=T, ne[1]=n_mels for conv_1d
  struct ggml_tensor* input = ggml_cont(ctx0, ggml_transpose(ctx0, input_raw));

  // --- Layer 0: TDNN (Conv1d(n_mels, C, 5) + BN + ReLU) ---
  struct ggml_tensor* cur = tdnn_block_forward(ctx0, input, weights_.layer0, 1, 1, eps_tensor);
  if (debug) debug_track(dbg, "layer0_out", cur);

  // --- SE-Res2Blocks + collect outputs for MFA ---
  std::vector<struct ggml_tensor*> block_outputs;
  for (int i = 0; i < hparams_.n_se_res2_blocks; i++) {
    cur = se_res2_block_forward(ctx0, cur, weights_.se_res2_blocks[i],
                                 hparams_.res2_scale,
                                 hparams_.kernel_sizes[i],
                                 hparams_.dilations[i], eps_tensor);
    if (debug) debug_track(dbg, "block" + std::to_string(i+1) + "_out", cur);
    block_outputs.push_back(cur);
  }

  // --- MFA: cat(block1_out, block2_out, block3_out) along channels -> TDNNBlock ---
  // Each block output: [T, C=1024], concat along dim=1 -> [T, 3*C=3072]
  struct ggml_tensor* mfa_in = block_outputs[0];
  for (int i = 1; i < (int)block_outputs.size(); i++) {
    mfa_in = ggml_concat(ctx0, mfa_in, block_outputs[i], 1);
  }
  cur = tdnn_block_forward(ctx0, mfa_in, weights_.mfa_conv, 1, 1, eps_tensor);
  // cur: [T, 3072]
  if (debug) debug_track(dbg, "mfa_out", cur);

  int T_out = cur->ne[0];
  int C_mfa = cur->ne[1];  // 3072

  // --- ASP (Attentive Statistical Pooling) with global_context ---
  // Compute global mean and std over time
  // pool_1d operates on ne[0] (time dim): [T, C] -> [1, C]
  struct ggml_tensor* g_mean = ggml_pool_1d(ctx0, cur, GGML_OP_POOL_AVG, T_out, T_out, 0);
  struct ggml_tensor* cur_sq = ggml_mul(ctx0, cur, cur);
  struct ggml_tensor* g_sq_mean = ggml_pool_1d(ctx0, cur_sq, GGML_OP_POOL_AVG, T_out, T_out, 0);
  struct ggml_tensor* g_var = ggml_sub(ctx0, g_sq_mean, ggml_mul(ctx0, g_mean, g_mean));
  // Bessel correction: PyTorch x.std(dim=2) divides by (N-1), not N.
  // pool_avg gives E[X²] and E[X]² which are ÷N, so var is population var.
  // Multiply by N/(N-1) to get sample variance matching PyTorch.
  if (T_out > 1) {
    g_var = ggml_scale(ctx0, g_var, (float)T_out / (float)(T_out - 1));
  }
  g_var = ggml_relu(ctx0, g_var);
  struct ggml_tensor* g_std = ggml_sqrt(ctx0, ggml_add1(ctx0, g_var, small_eps_tensor));

  // Repeat [1, C] -> [T, C]
  struct ggml_tensor* g_mean_rep = ggml_repeat(ctx0, g_mean, cur);
  struct ggml_tensor* g_std_rep = ggml_repeat(ctx0, g_std, cur);

  // attn_input = cat(cur, g_mean_rep, g_std_rep) along channels -> [T, C*3=9216]
  struct ggml_tensor* attn_in = ggml_concat(ctx0, cur, g_mean_rep, 1);
  attn_in = ggml_concat(ctx0, attn_in, g_std_rep, 1);

  // ASP tdnn: TDNNBlock(C*3=9216, attn_ch=128, 1) + ReLU
  // attn_in: [T, 9216], output: [T, 128]
  struct ggml_tensor* attn = tdnn_block_forward(ctx0, attn_in, weights_.asp_tdnn, 1, 1, eps_tensor);
  if (debug) debug_track(dbg, "asp_tdnn_out", attn);

  // ASP conv: Conv1d(attn_ch=128, C_mfa=3072, 1) — channel-dependent attention
  attn = ggml_conv_1d(ctx0, ensure_f16(ctx0, weights_.asp_conv_w), attn, 1, 0, 1);
  attn = ggml_reshape_2d(ctx0, attn, attn->ne[0], attn->ne[1]);
  attn = add_bias(ctx0, attn, weights_.asp_conv_b);
  // attn: [T, C_mfa=3072]

  // Softmax over time for each channel
  // ggml_soft_max operates on ne[0], which is T — perfect!
  attn = ggml_soft_max(ctx0, attn);
  if (debug) debug_track(dbg, "asp_softmax", attn);

  // Weighted mean: sum(cur * attn, dim=time) -> [1, C_mfa]
  struct ggml_tensor* weighted = ggml_mul(ctx0, cur, attn);
  struct ggml_tensor* w_mean = ggml_pool_1d(ctx0, weighted, GGML_OP_POOL_AVG, T_out, T_out, 0);
  w_mean = ggml_scale(ctx0, w_mean, (float)T_out);  // avg->sum

  // Weighted std: sqrt(sum(cur^2 * attn) - mean^2)
  struct ggml_tensor* weighted_sq = ggml_mul(ctx0, cur_sq, attn);
  struct ggml_tensor* w_sq_mean = ggml_pool_1d(ctx0, weighted_sq, GGML_OP_POOL_AVG, T_out, T_out, 0);
  w_sq_mean = ggml_scale(ctx0, w_sq_mean, (float)T_out);
  struct ggml_tensor* w_var = ggml_sub(ctx0, w_sq_mean, ggml_mul(ctx0, w_mean, w_mean));
  w_var = ggml_relu(ctx0, w_var);
  struct ggml_tensor* w_std = ggml_sqrt(ctx0, ggml_add1(ctx0, w_var, small_eps_tensor));

  // Concatenate mean and std: [1, C_mfa*2=6144]
  struct ggml_tensor* pooled = ggml_concat(ctx0, w_mean, w_std, 1);
  if (debug) debug_track(dbg, "asp_pooled", pooled);

  // ASP BatchNorm
  pooled = batch_norm_1d(ctx0, pooled, weights_.asp_bn_w, weights_.asp_bn_b,
                          weights_.asp_bn_mean, weights_.asp_bn_var, eps_tensor);
  if (debug) debug_track(dbg, "asp_bn_out", pooled);

  // Final FC: Conv1d(C*2=6144, emb_dim=192, 1)
  struct ggml_tensor* emb = ggml_conv_1d(ctx0, ensure_f16(ctx0, weights_.fc_w), pooled, 1, 0, 1);
  emb = ggml_reshape_2d(ctx0, emb, emb->ne[0], emb->ne[1]);
  emb = add_bias(ctx0, emb, weights_.fc_b);
  // emb: [1, 192]

  ggml_set_name(emb, "embedding_out");
  ggml_set_output(emb);
  ggml_build_forward_expand(gf, emb);

  // Allocate and compute
  if (!ggml_backend_sched_alloc_graph(sched, gf)) {
    RS_LOG_ERR("ECAPA-TDNN: graph allocation failed");
    ggml_free(ctx0);
    return false;
  }

  // Set input tensor values after allocation
  ggml_backend_tensor_set(input_raw, frames_ptr->data(), 0,
                          frames_ptr->size() * sizeof(float));
  float eps_val = eps;
  ggml_backend_tensor_set(eps_tensor, &eps_val, 0, sizeof(float));
  float small_eps_val = 1e-12f;
  ggml_backend_tensor_set(small_eps_tensor, &small_eps_val, 0, sizeof(float));

  // Debug: dump fbank input before compute
  if (debug) {
    std::string path = spk_state.debug_dump_dir + "/fbank_input.bin";
    std::ofstream f(path, std::ios::binary);
    f.write(reinterpret_cast<const char*>(frames_ptr->data()),
            frames_ptr->size() * sizeof(float));
    f.close();
    std::string spath = spk_state.debug_dump_dir + "/fbank_input.shape";
    std::ofstream sf(spath);
    // AudioProcessor output is [T, n_mels] row-major
    sf << T << "," << n_mels << std::endl;
    sf.close();
    RS_LOG_INFO("  DEBUG fbank_input: [%d, %d] (%zu floats)", T, n_mels,
                frames_ptr->size());
  }

  if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
    RS_LOG_ERR("ECAPA-TDNN: graph compute failed");
    ggml_free(ctx0);
    return false;
  }

  // Read embedding and L2-normalize on CPU
  spk_state.embedding.resize(emb_dim);
  ggml_backend_tensor_get(emb, spk_state.embedding.data(), 0, emb_dim * sizeof(float));

  // Debug: dump all tracked tensors
  if (debug) {
    RS_LOG_INFO("ECAPA-TDNN debug dump to: %s", spk_state.debug_dump_dir.c_str());
    for (auto& dt : dbg) {
      debug_dump_tensor(dt, sched, spk_state.debug_dump_dir);
    }
    // Also dump the final embedding (pre-normalization)
    DebugTensor emb_dt = {"fc_out", emb};
    debug_dump_tensor(emb_dt, sched, spk_state.debug_dump_dir);
  }

  // L2-normalize embedding (matching PyTorch F.normalize)
  float norm_sq = 0.0f;
  for (int i = 0; i < emb_dim; i++) {
    norm_sq += spk_state.embedding[i] * spk_state.embedding[i];
  }
  float inv_norm = 1.0f / (std::sqrt(norm_sq) + 1e-10f);
  for (int i = 0; i < emb_dim; i++) {
    spk_state.embedding[i] *= inv_norm;
  }

  ggml_free(ctx0);
  return true;
}

bool EcapaTdnnModel::Decode(RSState& state, ggml_backend_sched_t sched) {
  (void)state; (void)sched;
  return true;
}

std::string EcapaTdnnModel::GetTranscription(RSState& state) {
  (void)state;
  return "";
}

int EcapaTdnnModel::GetEmbedding(RSState& state, float** out_data) {
  auto& spk_state = static_cast<EcapaTdnnState&>(state);
  if (spk_state.embedding.empty()) return 0;
  *out_data = spk_state.embedding.data();
  return static_cast<int>(spk_state.embedding.size());
}

// =====================================================================
// Static registration
// =====================================================================
extern void rs_register_model_arch(const std::string& arch,
                                    std::function<std::shared_ptr<ISpeechModel>()> creator);
namespace {
struct EcapaTdnnRegistrar {
  EcapaTdnnRegistrar() {
    rs_register_model_arch("ecapa-tdnn", []() {
      return std::make_shared<EcapaTdnnModel>();
    });
  }
} global_ecapa_tdnn_reg;
}  // namespace

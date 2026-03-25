#!/usr/bin/env python3
"""
Check what fbank SpeechBrain actually uses for ECAPA-TDNN.
SpeechBrain's Fbank class uses torchaudio.compliance.kaldi.fbank internally,
but the hyperparams.yaml may specify different settings.

Let's check the actual hyperparams from the HuggingFace model.
"""
import os, sys, torch, numpy as np

# Download hyperparams.yaml from the model repo
ckpt_dir = os.path.join(os.path.dirname(__file__), "..", "..", "ecapa-raw")

# Check if hyperparams.yaml exists
hp_path = os.path.join(ckpt_dir, "hyperparams.yaml")
if not os.path.exists(hp_path):
    print("Downloading hyperparams.yaml...")
    try:
        os.environ["HF_ENDPOINT"] = "https://hf-mirror.com"
        from huggingface_hub import hf_hub_download
        hf_hub_download('speechbrain/spkrec-ecapa-voxceleb', 'hyperparams.yaml', local_dir=ckpt_dir)
    except Exception as e:
        print(f"Failed: {e}")

if os.path.exists(hp_path):
    print("=== hyperparams.yaml ===")
    with open(hp_path) as f:
        content = f.read()
    # Show relevant parts
    for line in content.split('\n'):
        line_lower = line.lower()
        if any(k in line_lower for k in ['fbank', 'mel', 'feature', 'n_mels', 'sample_rate', 
                                           'compute_features', 'mean_var', 'normalize',
                                           'deltas', 'context', 'input_norm']):
            print(f"  {line}")
    print("\n=== Full content ===")
    print(content[:3000])

# Also check if there's a mean_var_norm file
for fname in ['mean_var_norm_emb.ckpt', 'normalizer.ckpt', 'label_encoder.txt']:
    fpath = os.path.join(ckpt_dir, fname)
    if os.path.exists(fpath):
        print(f"\nFound: {fname}")
    else:
        print(f"Missing: {fname}")
        # Try to download
        try:
            os.environ["HF_ENDPOINT"] = "https://hf-mirror.com"
            from huggingface_hub import hf_hub_download
            hf_hub_download('speechbrain/spkrec-ecapa-voxceleb', fname, local_dir=ckpt_dir)
            print(f"  Downloaded {fname}")
        except:
            pass

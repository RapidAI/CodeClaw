#!/usr/bin/env python3
"""
Test ECAPA-TDNN with real human speech from LibriSpeech test-clean.
Downloads a few samples from different speakers to verify the model works.
"""
import os, sys, torch, numpy as np
import urllib.request

sys.path.insert(0, os.path.dirname(__file__))
from debug_ecapa_pytorch import ECAPA_TDNN, load_speechbrain_ckpt

OUT_DIR = os.path.join(os.path.dirname(__file__), "..", "test", "real_human")

# OpenSLR LibriSpeech test-clean samples (public domain)
# These are real human speakers with distinct voices
SAMPLES = {
    # Speaker 1089 (female)
    "spk1089_a": "https://www.openslr.org/resources/12/test-clean/1089/134686/1089-134686-0000.flac",
    "spk1089_b": "https://www.openslr.org/resources/12/test-clean/1089/134686/1089-134686-0001.flac",
    # Speaker 1188 (female)  
    "spk1188_a": "https://www.openslr.org/resources/12/test-clean/1188/133604/1188-133604-0000.flac",
    "spk1188_b": "https://www.openslr.org/resources/12/test-clean/1188/133604/1188-133604-0001.flac",
    # Speaker 1221 (male)
    "spk1221_a": "https://www.openslr.org/resources/12/test-clean/1221/135766/1221-135766-0000.flac",
    "spk1221_b": "https://www.openslr.org/resources/12/test-clean/1221/135766/1221-135766-0001.flac",
}

def download_and_convert(url, out_path):
    """Download audio and convert to 16kHz WAV."""
    if os.path.exists(out_path) and os.path.getsize(out_path) > 1000:
        return True
    
    tmp = out_path + ".tmp"
    try:
        print(f"  Downloading {url}...")
        urllib.request.urlretrieve(url, tmp)
        # Convert to WAV using soundfile
        import soundfile as sf
        audio, sr = sf.read(tmp)
        if sr != 16000:
            # Simple resample
            ratio = 16000 / sr
            n_out = int(len(audio) * ratio)
            audio = np.interp(np.linspace(0, len(audio)-1, n_out), np.arange(len(audio)), audio)
        sf.write(out_path, audio.astype(np.float32), 16000)
        os.remove(tmp)
        return True
    except Exception as e:
        print(f"  Failed: {e}")
        if os.path.exists(tmp):
            os.remove(tmp)
        return False

def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    
    # Try to download LibriSpeech samples
    # If that fails, generate synthetic signals with very different characteristics
    print("Attempting to download real human speech samples...")
    
    downloaded = {}
    for name, url in SAMPLES.items():
        path = os.path.join(OUT_DIR, f"{name}.wav")
        if download_and_convert(url, path):
            downloaded[name] = path
    
    if len(downloaded) < 4:
        print("\nCouldn't download LibriSpeech. Generating synthetic test signals instead.")
        print("Using very different signal types to verify model discrimination.")
        sr = 16000
        duration = 3.0
        t = np.linspace(0, duration, int(sr * duration))
        
        # Very different synthetic signals
        signals = {
            "low_male_sim": np.sin(2*np.pi*120*t) * 0.5 + np.sin(2*np.pi*240*t) * 0.3 + np.random.randn(len(t)) * 0.05,
            "high_female_sim": np.sin(2*np.pi*300*t) * 0.5 + np.sin(2*np.pi*600*t) * 0.3 + np.random.randn(len(t)) * 0.05,
            "noise_a": np.random.randn(len(t)) * 0.3,
            "noise_b": np.random.randn(len(t)) * 0.3,
            "chirp": np.sin(2*np.pi*(100 + 200*t/duration)*t) * 0.5,
            "pulse": (np.sin(2*np.pi*5*t) > 0.8).astype(np.float32) * np.sin(2*np.pi*200*t) * 0.5,
        }
        
        import soundfile as sf
        for name, sig in signals.items():
            path = os.path.join(OUT_DIR, f"{name}.wav")
            sf.write(path, sig.astype(np.float32), sr)
            downloaded[name] = path
    
    if not downloaded:
        print("No test data available.")
        return
    
    # Load model
    ckpt_path = os.path.join(os.path.dirname(__file__), "..", "..", "ecapa-raw", "embedding_model.ckpt")
    model = ECAPA_TDNN(n_mels=80, channels=1024, emb_dim=192, res2_scale=8)
    model = load_speechbrain_ckpt(model, ckpt_path)
    model.eval()
    
    # Compute embeddings
    embeddings = {}
    for name, path in sorted(downloaded.items()):
        try:
            import soundfile as sf
            audio, sr = sf.read(path)
            if sr != 16000:
                ratio = 16000 / sr
                n_out = int(len(audio) * ratio)
                audio = np.interp(np.linspace(0, len(audio)-1, n_out), np.arange(len(audio)), audio)
            
            waveform = torch.from_numpy(audio.astype(np.float32)).unsqueeze(0)
            
            try:
                import torchaudio
                feats = torchaudio.compliance.kaldi.fbank(
                    waveform, num_mel_bins=80, sample_frequency=16000,
                    frame_length=25.0, frame_shift=10.0,
                    window_type='hamming', use_energy=False,
                )
            except:
                from debug_ecapa_pytorch import compute_fbank_pytorch_from_array
                feats_np = compute_fbank_pytorch_from_array(audio.astype(np.float32), 16000)
                feats = torch.from_numpy(feats_np)
            
            feats = feats.unsqueeze(0)
            with torch.no_grad():
                emb, _ = model(feats, debug=False)
            emb_np = emb.squeeze(0).numpy()
            emb_np = emb_np / (np.linalg.norm(emb_np) + 1e-10)
            embeddings[name] = emb_np
            print(f"  {name:25s} norm={np.linalg.norm(emb.numpy()):.1f}")
        except Exception as e:
            print(f"  {name}: ERROR {e}")
    
    # Similarities
    names = sorted(embeddings.keys())
    print(f"\nAll pairwise cosine similarities:")
    for i in range(len(names)):
        for j in range(i+1, len(names)):
            cos = np.dot(embeddings[names[i]], embeddings[names[j]])
            spk_i = names[i].rsplit('_', 1)[0]
            spk_j = names[j].rsplit('_', 1)[0]
            tag = "SAME" if spk_i == spk_j else "DIFF"
            print(f"  {names[i]:25s} vs {names[j]:25s} => {cos:.4f} [{tag}]")

if __name__ == "__main__":
    main()

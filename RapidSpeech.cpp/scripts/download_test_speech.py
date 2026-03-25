#!/usr/bin/env python3
"""Download real speech WAV files for speaker verification testing.
Uses Mozilla Common Voice single-file downloads or generates pitch-shifted variants."""

import os
import sys
import struct
import wave
import math
import json
import subprocess

OUT_DIR = os.path.join(os.path.dirname(__file__), "..", "test", "real_speech")
SERVER = "http://localhost:8090"

# Try multiple sources for real speech WAVs
URLS = [
    # LibriSpeech mini samples from various mirrors
    ("spk_1260_0", "https://www.openslr.org/resources/12/test-clean-wav/1260-141472-0000.wav"),
    ("spk_1260_1", "https://www.openslr.org/resources/12/test-clean-wav/1260-141472-0001.wav"),
    # Alternative: use raw GitHub content from speech projects
]

def generate_from_asr_test():
    """Use the existing ASR test WAV and create pitch-shifted versions as different 'speakers'."""
    # Check if we have any existing WAV in the assets or test directory
    asr_test = os.path.join(os.path.dirname(__file__), "..", "assets")
    wavs = []
    if os.path.exists(asr_test):
        for f in os.listdir(asr_test):
            if f.endswith(".wav"):
                wavs.append(os.path.join(asr_test, f))
    
    test_dir = os.path.join(os.path.dirname(__file__), "..", "test")
    if os.path.exists(test_dir):
        for f in os.listdir(test_dir):
            if f.endswith(".wav"):
                wavs.append(os.path.join(test_dir, f))
    
    return wavs

def pitch_shift_wav(in_path, out_path, semitones):
    """Pitch shift a WAV file using resampling trick (change speed then resample back).
    This changes the perceived speaker identity."""
    try:
        import torch
        import torchaudio
        waveform, sr = torchaudio.load(in_path)
        # Speed change factor (pitch up = speed up then resample down)
        factor = 2.0 ** (semitones / 12.0)
        # Resample to change pitch
        new_sr = int(sr * factor)
        # Resample back to original sr
        resampled = torchaudio.functional.resample(waveform, new_sr, sr)
        torchaudio.save(out_path, resampled, sr)
        return True
    except Exception as e:
        print(f"  pitch_shift error: {e}")
        return False

def generate_tts_samples():
    """Generate speech samples using edge-tts or gTTS."""
    try:
        # Try using torch to generate simple speech-like signals with different characteristics
        import torch
        import torchaudio
        
        sr = 16000
        duration = 3.0
        n = int(sr * duration)
        t = torch.linspace(0, duration, n)
        
        speakers = {}
        # Generate voiced signals with different F0 patterns (simulating different speakers)
        configs = [
            ("deep_male", 85, [600, 1000, 2400]),
            ("avg_male", 125, [700, 1200, 2600]),
            ("high_male", 165, [750, 1300, 2700]),
            ("avg_female", 220, [850, 1500, 2900]),
            ("high_female", 280, [950, 1700, 3100]),
        ]
        
        for name, f0, formants in configs:
            speakers[name] = []
            for utt in range(2):
                torch.manual_seed(hash(f"{name}_{utt}") % 2**31)
                signal = torch.zeros(n)
                # Generate harmonics
                for h in range(1, 10):
                    freq = f0 * h
                    # Formant envelope
                    amp = 0.0
                    for fk in formants:
                        bw = 80
                        amp += 1.0 / (1.0 + ((freq - fk) / bw) ** 2)
                    amp = amp / len(formants) * 0.25 / h
                    # Add jitter for naturalness
                    phase = torch.rand(1).item() * 2 * math.pi
                    jitter = 1.0 + 0.01 * torch.randn(n)
                    signal += amp * torch.sin(2 * math.pi * freq * t * jitter + phase)
                
                # Add aspiration noise
                signal += 0.015 * torch.randn(n)
                
                # Amplitude envelope
                env = torch.ones(n)
                fade = int(0.05 * sr)
                env[:fade] = torch.linspace(0, 1, fade)
                env[-fade:] = torch.linspace(1, 0, fade)
                signal = signal * env
                
                # Normalize
                signal = signal / (signal.abs().max() + 1e-6) * 0.8
                
                path = os.path.join(OUT_DIR, f"{name}_{utt}.wav")
                torchaudio.save(path, signal.unsqueeze(0), sr)
                speakers[name].append(path)
        
        return speakers
    except ImportError:
        return None

def api_verify(wav1, wav2):
    r = subprocess.run(
        ["curl.exe", "-s", "-X", "POST", f"{SERVER}/v1/speaker-verify",
         "-F", f"audio1=@{wav1}", "-F", f"audio2=@{wav2}"],
        capture_output=True, text=True, timeout=60
    )
    try:
        return json.loads(r.stdout)
    except:
        return None

def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    
    # First try: find existing real WAVs and pitch-shift them
    existing = generate_from_asr_test()
    
    if existing:
        print(f"Found {len(existing)} existing WAV files, creating pitch-shifted variants...")
        speakers = {}
        src = existing[0]
        shifts = [0, -4, -8, 4, 8]  # semitones
        names = ["original", "lower1", "lower2", "higher1", "higher2"]
        for name, shift in zip(names, shifts):
            speakers[name] = []
            for utt in range(2):
                out = os.path.join(OUT_DIR, f"{name}_{utt}.wav")
                actual_shift = shift + (utt * 0.5)  # slight variation per utterance
                if shift == 0 and utt == 0:
                    # Just copy
                    import shutil
                    shutil.copy2(src, out)
                else:
                    if not pitch_shift_wav(src if utt == 0 else (existing[min(utt, len(existing)-1)]), out, actual_shift):
                        continue
                speakers[name].append(out)
        
        if len(speakers) >= 3:
            run_test(speakers)
            return
    
    # Fallback: generate synthetic speech-like signals
    print("Generating synthetic speaker samples with torchaudio...")
    speakers = generate_tts_samples()
    if speakers:
        run_test(speakers)
    else:
        print("No torchaudio available. Cannot generate test data.")

def run_test(speakers):
    spk_ids = sorted(speakers.keys())
    print(f"\n=== Speaker Verification Test ({len(spk_ids)} speakers) ===\n")
    
    print("--- Same Speaker ---")
    same_scores = []
    for spk in spk_ids:
        files = speakers[spk]
        if len(files) >= 2:
            r = api_verify(files[0], files[1])
            if r and "score" in r:
                same_scores.append(r["score"])
                print(f"  {spk:15s} => {r['score']:.4f}")
    
    print("\n--- Different Speakers ---")
    diff_scores = []
    for i in range(len(spk_ids)):
        for j in range(i+1, len(spk_ids)):
            a, b = spk_ids[i], spk_ids[j]
            r = api_verify(speakers[a][0], speakers[b][0])
            if r and "score" in r:
                diff_scores.append(r["score"])
                print(f"  {a:15s} vs {b:15s} => {r['score']:.4f}")
    
    print("\n=== Summary ===")
    if same_scores:
        print(f"Same:  min={min(same_scores):.4f} max={max(same_scores):.4f} avg={sum(same_scores)/len(same_scores):.4f}")
    if diff_scores:
        print(f"Diff:  min={min(diff_scores):.4f} max={max(diff_scores):.4f} avg={sum(diff_scores)/len(diff_scores):.4f}")
    if same_scores and diff_scores:
        gap = min(same_scores) - max(diff_scores)
        print(f"Gap: {gap:.4f}  {'PASS' if gap > 0 else 'OVERLAP'}")

if __name__ == "__main__":
    main()

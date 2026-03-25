#!/usr/bin/env python3
"""Generate real speech with different voices via edge-tts, then test speaker verification."""

import asyncio
import os
import sys
import json
import subprocess
import tempfile

SERVER = "http://localhost:8090"
OUT_DIR = os.path.join(os.path.dirname(__file__), "..", "test", "real_speech")

# Different edge-tts voices (real neural TTS, distinct speakers)
VOICES = {
    "zh_male_yunjian":   "zh-CN-YunjianNeural",
    "zh_male_yunxi":     "zh-CN-YunxiNeural",
    "zh_female_xiaoxiao":"zh-CN-XiaoxiaoNeural",
    "zh_female_xiaoyi":  "zh-CN-XiaoyiNeural",
    "en_male_guy":       "en-US-GuyNeural",
    "en_female_jenny":   "en-US-JennyNeural",
}

# Two different texts per voice (same speaker, different content)
TEXTS = {
    "zh": [
        "今天天气真不错，我们一起去公园散步吧。",
        "人工智能技术正在改变我们的生活方式。",
    ],
    "en": [
        "The weather is really nice today, let us go for a walk in the park.",
        "Artificial intelligence is changing the way we live our lives.",
    ],
}

async def generate_speech(voice, text, out_path):
    """Generate speech using edge-tts."""
    import edge_tts
    communicate = edge_tts.Communicate(text, voice)
    # edge-tts outputs mp3, we need to convert to wav
    mp3_path = out_path.replace(".wav", ".mp3")
    await communicate.save(mp3_path)
    # Convert mp3 to 16kHz mono WAV using ffmpeg or torchaudio
    try:
        import torchaudio
        waveform, sr = torchaudio.load(mp3_path)
        if waveform.shape[0] > 1:
            waveform = waveform.mean(dim=0, keepdim=True)
        if sr != 16000:
            waveform = torchaudio.functional.resample(waveform, sr, 16000)
        torchaudio.save(out_path, waveform, 16000)
    except:
        os.system(f'ffmpeg -y -i "{mp3_path}" -ar 16000 -ac 1 "{out_path}" -loglevel quiet')
    if os.path.exists(mp3_path):
        os.remove(mp3_path)

async def generate_all():
    """Generate all speech samples."""
    os.makedirs(OUT_DIR, exist_ok=True)
    speakers = {}
    
    for name, voice in VOICES.items():
        lang = "zh" if name.startswith("zh") else "en"
        texts = TEXTS[lang]
        speakers[name] = []
        
        for i, text in enumerate(texts):
            path = os.path.join(OUT_DIR, f"{name}_{i}.wav")
            if os.path.exists(path) and os.path.getsize(path) > 1000:
                print(f"  {name}_{i}.wav exists, skip")
                speakers[name].append(path)
                continue
            print(f"  Generating {name}_{i}.wav ({voice})...")
            try:
                await generate_speech(voice, text, path)
                if os.path.exists(path) and os.path.getsize(path) > 100:
                    speakers[name].append(path)
                    print(f"    OK ({os.path.getsize(path)} bytes)")
                else:
                    print(f"    FAILED (no output)")
            except Exception as e:
                print(f"    ERROR: {e}")
    
    return speakers

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

def run_test(speakers):
    spk_ids = sorted(k for k, v in speakers.items() if len(v) >= 2)
    print(f"\n=== Speaker Verification ({len(spk_ids)} speakers) ===\n")
    
    print("--- Same Speaker (expect HIGH) ---")
    same = []
    for s in spk_ids:
        r = api_verify(speakers[s][0], speakers[s][1])
        if r and "score" in r:
            same.append(r["score"])
            print(f"  {s:25s} => {r['score']:.4f}")
    
    print("\n--- Different Speakers (expect LOW) ---")
    diff = []
    for i in range(len(spk_ids)):
        for j in range(i+1, len(spk_ids)):
            a, b = spk_ids[i], spk_ids[j]
            r = api_verify(speakers[a][0], speakers[b][0])
            if r and "score" in r:
                diff.append(r["score"])
                label = "SAME_LANG" if a[:2] == b[:2] else "CROSS_LANG"
                print(f"  {a:25s} vs {b:25s} => {r['score']:.4f}  [{label}]")
    
    print("\n=== Summary ===")
    if same:
        print(f"Same speaker:  min={min(same):.4f} max={max(same):.4f} avg={sum(same)/len(same):.4f}")
    if diff:
        print(f"Diff speaker:  min={min(diff):.4f} max={max(diff):.4f} avg={sum(diff)/len(diff):.4f}")
    if same and diff:
        gap = min(same) - max(diff)
        print(f"Gap (min_same - max_diff): {gap:.4f}")
        if gap > 0.05:
            print("EXCELLENT: Clear separation!")
        elif gap > 0:
            print("GOOD: Positive gap, model distinguishes speakers.")
        else:
            print("MARGINAL: Some overlap, but may still work with tuned threshold.")

def main():
    print("Generating real speech samples with edge-tts...")
    speakers = asyncio.run(generate_all())
    run_test(speakers)

if __name__ == "__main__":
    main()

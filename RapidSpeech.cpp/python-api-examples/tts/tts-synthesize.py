#!/usr/bin/env python3
"""
RapidSpeech TTS synthesis example.

Usage:
  # Basic synthesis (default voice)
  python tts-synthesize.py --model /path/to/openvoice2.gguf --text "Hello world"

  # With voice cloning
  python tts-synthesize.py --model /path/to/openvoice2.gguf \
      --text "Hello world" --reference ref.wav --output out.wav

  # Streaming mode (prints chunk info)
  python tts-synthesize.py --model /path/to/openvoice2.gguf \
      --text "Hello world" --streaming
"""

import argparse
import struct
import sys
import numpy as np

try:
    import rapidspeech
except ImportError:
    print("Error: rapidspeech module not found. Build with -DRS_ENABLE_PYTHON=ON")
    sys.exit(1)


def read_wav_pcm(path: str) -> tuple:
    """Read a WAV file and return (pcm_float32, sample_rate)."""
    import wave
    with wave.open(path, "rb") as wf:
        assert wf.getsampwidth() == 2, "Only 16-bit WAV supported"
        sr = wf.getframerate()
        frames = wf.readframes(wf.getnframes())
    pcm_i16 = np.frombuffer(frames, dtype=np.int16)
    return pcm_i16.astype(np.float32) / 32768.0, sr


def write_wav(path: str, pcm: np.ndarray, sample_rate: int = 22050):
    """Write PCM float32 to a 16-bit WAV file."""
    pcm_i16 = np.clip(pcm * 32768.0, -32768, 32767).astype(np.int16)
    import wave
    with wave.open(path, "wb") as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)
        wf.setframerate(sample_rate)
        wf.writeframes(pcm_i16.tobytes())


def main():
    parser = argparse.ArgumentParser(description="RapidSpeech TTS synthesis")
    parser.add_argument("--model", required=True, help="Path to OpenVoice2 GGUF model")
    parser.add_argument("--text", required=True, help="Text to synthesize")
    parser.add_argument("--reference", default=None, help="Reference WAV for voice cloning")
    parser.add_argument("--output", default="output.wav", help="Output WAV path")
    parser.add_argument("--threads", type=int, default=4, help="Number of threads")
    parser.add_argument("--streaming", action="store_true", help="Use streaming mode")
    parser.add_argument("--sample-rate", type=int, default=22050, help="Output sample rate")
    args = parser.parse_args()

    # Initialize synthesizer
    tts = rapidspeech.tts_synthesizer(args.model, n_threads=args.threads)
    print(f"Model loaded: {args.model}")

    # Optional: set reference audio for voice cloning
    if args.reference:
        ref_pcm, ref_sr = read_wav_pcm(args.reference)
        tts.set_reference(ref_pcm, ref_sr)
        print(f"Reference audio set: {args.reference} ({ref_sr} Hz, {len(ref_pcm)} samples)")

    if args.streaming:
        # Streaming mode: get audio in chunks
        chunks = tts.synthesize_streaming(args.text)
        print(f"Received {len(chunks)} audio chunks:")
        all_pcm = []
        for i, chunk in enumerate(chunks):
            dur_ms = len(chunk) / args.sample_rate * 1000
            print(f"  chunk {i}: {len(chunk)} samples ({dur_ms:.0f} ms)")
            all_pcm.append(chunk)
        pcm = np.concatenate(all_pcm) if all_pcm else np.array([], dtype=np.float32)
    else:
        # Batch mode: get full audio at once
        pcm = tts.synthesize(args.text)

    if len(pcm) == 0:
        print("Warning: no audio generated")
        return

    duration = len(pcm) / args.sample_rate
    write_wav(args.output, pcm, args.sample_rate)
    print(f"Saved: {args.output} ({duration:.2f}s, {len(pcm)} samples)")


if __name__ == "__main__":
    main()

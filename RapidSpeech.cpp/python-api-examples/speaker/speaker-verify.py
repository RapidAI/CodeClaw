import argparse
import array
import struct

import numpy as np
import rapidspeech


def read_wav(file_path):
    with open(file_path, "rb") as f:
        data = f.read()

    info = data[:44]
    (
        name, data_lengths, _, _, _, _,
        channels, sample_rate, bit_rate, block_length, sample_bit,
        _, pcm_length,
    ) = struct.unpack_from("<4sL4s4sLHHLLHH4sL", info)

    short_array = array.array("h")
    short_array.frombytes(data[44:])
    pcm = np.array(short_array, dtype="float32") / (1 << 15)

    if channels > 1:
        pcm = pcm.reshape(-1, channels)[:, 0]  # take first channel
    return pcm


def main():
    parser = argparse.ArgumentParser(description="RapidSpeech Speaker Verification Demo")
    parser.add_argument("--model", required=True, help="ECAPA-TDNN 模型文件 (.gguf)")
    parser.add_argument("--wav1", required=True, help="第一段音频 (16kHz PCM wav)")
    parser.add_argument("--wav2", required=True, help="第二段音频 (16kHz PCM wav)")
    parser.add_argument("--threshold", type=float, default=0.5, help="判定阈值 (默认 0.5)")
    parser.add_argument("--threads", type=int, default=4, help="线程数")
    parser.add_argument("--gpu", type=int, default=1, help="是否使用 GPU, 1=用, 0=不用")
    args = parser.parse_args()

    # --- 方式 1: 使用 SpeakerVerifier (一步到位) ---
    verifier = rapidspeech.speaker_verifier(
        model_path=args.model, n_threads=args.threads, use_gpu=bool(args.gpu)
    )

    pcm1 = np.ascontiguousarray(read_wav(args.wav1), dtype=np.float32)
    pcm2 = np.ascontiguousarray(read_wav(args.wav2), dtype=np.float32)

    result = verifier.verify(pcm1, pcm2, threshold=args.threshold)
    print(f"Score       : {result['score']:.6f}")
    print(f"Same speaker: {result['same_speaker']}")
    print(f"Threshold   : {result['threshold']}")

    # --- 方式 2: 使用 SpeakerEmbedder (分步提取 embedding) ---
    print("\n--- Embedding 模式 ---")
    embedder = rapidspeech.speaker_embedder(
        model_path=args.model, n_threads=args.threads, use_gpu=bool(args.gpu)
    )

    emb1 = embedder.embed(pcm1)
    emb2 = embedder.embed(pcm2)
    print(f"Embedding dim: {len(emb1)}")

    # 手动计算 cosine similarity
    score = np.dot(emb1, emb2) / (np.linalg.norm(emb1) * np.linalg.norm(emb2))
    print(f"Cosine sim   : {score:.6f}")


if __name__ == "__main__":
    main()

// corelib/audioconv/opus.go — OGG/Opus to WAV converter.
//
// Uses github.com/pion/opus (pure Go) for Opus decoding and the local
// OGG demuxer to extract packets. Feishu and Telegram voice messages
// use OGG/Opus encoding.
package audioconv

import (
	"fmt"

	"github.com/pion/opus"
)

// opusToWAV decodes OGG/Opus audio data to 16kHz mono 16-bit WAV.
func opusToWAV(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audioconv: empty opus data")
	}

	// Extract Opus packets from OGG container.
	packets, err := extractOggOpusPackets(data)
	if err != nil {
		return nil, fmt.Errorf("audioconv: ogg demux failed: %w", err)
	}

	// Decode each Opus packet to S16LE PCM.
	// Opus internally always operates at 48kHz; pion/opus decodes at the
	// bandwidth's effective sample rate (returned per-packet).
	decoder := opus.NewDecoder()

	// Max frame: 60ms at 48kHz = 2880 samples × 2 bytes = 5760 bytes.
	const maxFrameBytes = 5760
	frameBuf := make([]byte, maxFrameBytes)

	var allPCM []byte
	decodeSampleRate := 0 // determined from first successful decode

	for _, pkt := range packets {
		bw, _, err := decoder.Decode(pkt, frameBuf)
		if err != nil {
			// Skip bad packets rather than failing the whole file.
			continue
		}
		pktRate := bw.SampleRate()
		if decodeSampleRate == 0 {
			decodeSampleRate = pktRate
		}
		// Calculate decoded frame size from TOC byte using the actual decode rate.
		n := opusFrameBytes(pkt, pktRate)
		if n > 0 && n <= len(frameBuf) {
			allPCM = append(allPCM, frameBuf[:n]...)
		}
	}

	if len(allPCM) == 0 {
		return nil, fmt.Errorf("audioconv: opus decode produced empty PCM")
	}
	if decodeSampleRate == 0 {
		decodeSampleRate = 48000
	}

	// Resample to 16kHz if needed.
	if decodeSampleRate != TargetSampleRate {
		allPCM = resampleS16(allPCM, decodeSampleRate, TargetSampleRate)
	}

	return pcmToWAV(allPCM, TargetSampleRate, TargetChannels)
}


// opusFrameBytes estimates the decoded PCM byte count for an Opus packet
// based on its TOC byte. Returns bytes (S16LE mono).
func opusFrameBytes(pkt []byte, sampleRate int) int {
	if len(pkt) == 0 {
		return 0
	}
	toc := pkt[0]
	config := (toc >> 3) & 0x1F

	// Frame duration in microseconds based on config number.
	var frameDurUs int
	switch {
	case config <= 11:
		// SILK-only: configs 0-3 = 10,20,40,60ms; 4-7 same; 8-11 same
		switch config % 4 {
		case 0:
			frameDurUs = 10000
		case 1:
			frameDurUs = 20000
		case 2:
			frameDurUs = 40000
		case 3:
			frameDurUs = 60000
		}
	case config <= 15:
		// Hybrid: 12-13 = 10,20ms; 14-15 = 10,20ms
		if config%2 == 0 {
			frameDurUs = 10000
		} else {
			frameDurUs = 20000
		}
	default:
		// CELT-only: 16-19, 20-23, 24-27, 28-31 = 2.5,5,10,20ms
		switch config % 4 {
		case 0:
			frameDurUs = 2500
		case 1:
			frameDurUs = 5000
		case 2:
			frameDurUs = 10000
		case 3:
			frameDurUs = 20000
		}
	}

	// Number of frames per packet (from code field, bits 0-1 of TOC).
	code := toc & 0x03
	numFrames := 1
	switch code {
	case 0:
		numFrames = 1
	case 1, 2:
		numFrames = 2
	case 3:
		// Arbitrary number of frames — read from next byte.
		if len(pkt) > 1 {
			numFrames = int(pkt[1] & 0x3F)
		}
	}

	samples := sampleRate * frameDurUs / 1000000 * numFrames
	return samples * 2 // S16LE = 2 bytes per sample
}

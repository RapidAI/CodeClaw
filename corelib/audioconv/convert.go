// corelib/audioconv/convert.go — Unified IM voice → WAV converter.
//
// Converts voice messages from various IM platforms to 16kHz mono 16-bit WAV
// suitable for ASR (speech recognition).
//
// Supported input formats:
//   - silk / silk_v3: WeChat, QQ voice messages
//   - ogg / opus:     Feishu, Telegram voice messages
//   - wav:            Already WAV, returned as-is (or resampled if needed)
package audioconv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"strings"
)

// Format constants for the format parameter.
const (
	FormatSilk = "silk"
	FormatOGG  = "ogg"
	FormatOpus = "opus"
	FormatWAV  = "wav"
)

// ToWAV converts voice audio data to 16kHz mono 16-bit WAV.
// The format parameter hints at the source format ("silk", "ogg", "opus", "wav").
// If format is empty, auto-detection is attempted.
func ToWAV(data []byte, format string) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audioconv: empty input data")
	}

	if format == "" {
		format = detectFormat(data)
	}

	switch strings.ToLower(format) {
	case FormatSilk, "silk_v3", "amr":
		return silkToWAV(data)
	case FormatOGG, FormatOpus, "oga":
		return opusToWAV(data)
	case FormatWAV:
		return ensureWAVFormat(data)
	default:
		return nil, fmt.Errorf("audioconv: unsupported format %q", format)
	}
}

// detectFormat tries to identify the audio format from magic bytes.
func detectFormat(data []byte) string {
	// Silk v3 (with or without 0x02 prefix)
	if bytes.HasPrefix(data, []byte("#!SILK_V3")) {
		return FormatSilk
	}
	if len(data) > 1 && data[0] == 0x02 && bytes.HasPrefix(data[1:], []byte("#!SILK_V3")) {
		return FormatSilk
	}
	// OGG container
	if bytes.HasPrefix(data, []byte("OggS")) {
		return FormatOGG
	}
	// WAV / RIFF
	if bytes.HasPrefix(data, []byte("RIFF")) && len(data) > 11 && string(data[8:12]) == "WAVE" {
		return FormatWAV
	}
	return ""
}

// ensureWAVFormat checks if the WAV is already 16kHz/mono/16bit. If not,
// it extracts the PCM, resamples, and re-wraps.
func ensureWAVFormat(data []byte) ([]byte, error) {
	if len(data) < 44 {
		return nil, fmt.Errorf("audioconv: WAV too short")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("audioconv: not a valid WAV file")
	}

	// Search for "fmt " chunk (don't assume fixed offset).
	channels, sampleRate, bitsPerSample, err := parseFmtChunk(data)
	if err != nil {
		return nil, err
	}

	if sampleRate == TargetSampleRate && channels == TargetChannels && bitsPerSample == TargetBitsPerSamp {
		return data, nil // Already in target format.
	}

	// Find data chunk
	pcm, err := extractWAVData(data)
	if err != nil {
		return nil, err
	}

	// Convert stereo to mono if needed
	if channels == 2 && bitsPerSample == 16 {
		pcm = stereoToMono(pcm)
	}

	// Resample if needed
	if sampleRate != TargetSampleRate && bitsPerSample == 16 {
		pcm = resampleS16(pcm, sampleRate, TargetSampleRate)
	}

	log.Printf("[audioconv] WAV resampled: %dHz/%dch → %dHz/1ch", sampleRate, channels, TargetSampleRate)
	return pcmToWAV(pcm, TargetSampleRate, TargetChannels)
}

// parseFmtChunk searches for the "fmt " chunk in a WAV file and returns
// channels, sampleRate, and bitsPerSample.
func parseFmtChunk(data []byte) (channels, sampleRate, bitsPerSample int, err error) {
	for i := 12; i+8 < len(data); {
		chunkID := string(data[i : i+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if chunkID == "fmt " && chunkSize >= 16 && i+8+16 <= len(data) {
			channels = int(binary.LittleEndian.Uint16(data[i+10 : i+12]))
			sampleRate = int(binary.LittleEndian.Uint32(data[i+12 : i+16]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[i+22 : i+24]))
			return channels, sampleRate, bitsPerSample, nil
		}
		i += 8 + chunkSize
		if chunkSize%2 != 0 {
			i++ // WAV chunk padding to even boundary
		}
	}
	return 0, 0, 0, fmt.Errorf("audioconv: WAV fmt chunk not found")
}

// extractWAVData finds and returns the raw PCM data from a WAV file.
func extractWAVData(data []byte) ([]byte, error) {
	// Search for "data" chunk
	for i := 12; i+8 < len(data); {
		chunkID := string(data[i : i+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if chunkID == "data" {
			start := i + 8
			end := start + chunkSize
			if end > len(data) {
				end = len(data)
			}
			return data[start:end], nil
		}
		i += 8 + chunkSize
		if chunkSize%2 != 0 {
			i++ // WAV chunk padding to even boundary
		}
	}
	return nil, fmt.Errorf("audioconv: WAV data chunk not found")
}

// stereoToMono converts interleaved stereo S16LE to mono by averaging.
func stereoToMono(pcm []byte) []byte {
	samples := len(pcm) / 4 // 2 channels × 2 bytes
	out := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		l := int(readS16LE(pcm, i*2))
		r := int(readS16LE(pcm, i*2+1))
		avg := int16((l + r) / 2)
		writeS16LE(out, i, avg)
	}
	return out
}

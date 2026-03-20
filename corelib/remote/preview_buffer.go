package remote

import (
	"strings"
	"time"
)

// RingPreviewBuffer is a ring-buffer based PreviewBuffer implementation.
type RingPreviewBuffer struct {
	maxLines int
	seq      int64
	lines    []string
}

// NewRingPreviewBuffer creates a new ring preview buffer.
func NewRingPreviewBuffer(maxLines int) *RingPreviewBuffer {
	return &RingPreviewBuffer{
		maxLines: maxLines,
		lines:    make([]string, 0, maxLines),
	}
}

// Append implements PreviewBuffer.
func (b *RingPreviewBuffer) Append(sessionID string, lines []string) *SessionPreviewDelta {
	filtered := compressPreviewLines(lines)
	if len(filtered) == 0 {
		return nil
	}

	b.seq += int64(len(filtered))
	b.lines = append(b.lines, filtered...)
	if len(b.lines) > b.maxLines {
		b.lines = b.lines[len(b.lines)-b.maxLines:]
	}

	return &SessionPreviewDelta{
		SessionID:   sessionID,
		OutputSeq:   b.seq,
		AppendLines: filtered,
		UpdatedAt:   time.Now().Unix(),
	}
}

func compressPreviewLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned := strings.TrimRight(line, " \t")
		if cleaned == "" || IsNoiseLine(cleaned) {
			continue
		}
		out = append(out, cleaned)
	}
	return out
}

package sse

import (
	"context"
	"strings"
	"testing"
)

func TestStartParsedLinePumpParsesAndStops(t *testing.T) {
	body := strings.NewReader("data: {\"p\":\"response/content\",\"v\":\"hi\"}\n\ndata: [DONE]\n")
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0, 2)
	for r := range results {
		collected = append(collected, r)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected scanner error: %v", err)
	}
	if len(collected) < 2 {
		t.Fatalf("expected at least 2 parsed results, got %d", len(collected))
	}
	if !collected[0].Parsed || len(collected[0].Parts) == 0 {
		t.Fatalf("expected first line to contain parsed content")
	}
	last := collected[len(collected)-1]
	if !last.Parsed || !last.Stop {
		t.Fatalf("expected last line to stop stream, got parsed=%v stop=%v", last.Parsed, last.Stop)
	}
}

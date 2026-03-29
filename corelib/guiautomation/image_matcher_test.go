package guiautomation

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// encodePNG encodes an image to base64 PNG string.
func encodePNG(img image.Image) string {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// solidImage creates a solid-color image of the given size.
func solidImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// embedImage draws src onto dst at the given offset.
func embedImage(dst *image.RGBA, src *image.RGBA, ox, oy int) {
	sb := src.Bounds()
	for y := sb.Min.Y; y < sb.Max.Y; y++ {
		for x := sb.Min.X; x < sb.Max.X; x++ {
			dst.Set(ox+x-sb.Min.X, oy+y-sb.Min.Y, src.At(x, y))
		}
	}
}

func TestFindByImage_ExactMatch(t *testing.T) {
	// Create a 100x100 gray screen with a 20x20 red patch at (30, 40).
	screen := solidImage(100, 100, color.RGBA{128, 128, 128, 255})
	patch := solidImage(20, 20, color.RGBA{255, 0, 0, 255})
	embedImage(screen, patch, 30, 40)

	screenB64 := encodePNG(screen)
	refB64 := encodePNG(patch)

	matcher := NewImageMatcher(nil, func() (string, error) {
		return screenB64, nil
	})

	result, err := matcher.FindByImage(refB64, nil)
	if err != nil {
		t.Fatalf("FindByImage error: %v", err)
	}
	if !result.Found {
		t.Fatalf("expected Found=true, got false (confidence=%.4f)", result.Confidence)
	}
	if result.Confidence < confidenceThreshold {
		t.Errorf("confidence %.4f below threshold %.2f", result.Confidence, confidenceThreshold)
	}
	// Center should be at (30+10, 40+10) = (40, 50).
	if result.X != 40 || result.Y != 50 {
		t.Errorf("expected center (40,50), got (%d,%d)", result.X, result.Y)
	}
}

func TestFindByImage_NoMatch(t *testing.T) {
	// Screen is all gray, reference is all red — they should not match well.
	screen := solidImage(50, 50, color.RGBA{128, 128, 128, 255})
	ref := solidImage(10, 10, color.RGBA{255, 0, 0, 255})

	screenB64 := encodePNG(screen)
	refB64 := encodePNG(ref)

	matcher := NewImageMatcher(nil, func() (string, error) {
		return screenB64, nil
	})

	result, err := matcher.FindByImage(refB64, nil)
	if err != nil {
		t.Fatalf("FindByImage error: %v", err)
	}
	if result.Found {
		t.Errorf("expected Found=false for non-matching image, got Found=true confidence=%.4f", result.Confidence)
	}
}

func TestFindByImage_WithSearchRegion(t *testing.T) {
	// 200x200 screen, red patch at (150, 150). Search region excludes it.
	screen := solidImage(200, 200, color.RGBA{128, 128, 128, 255})
	patch := solidImage(20, 20, color.RGBA{255, 0, 0, 255})
	embedImage(screen, patch, 150, 150)

	screenB64 := encodePNG(screen)
	refB64 := encodePNG(patch)

	matcher := NewImageMatcher(nil, func() (string, error) {
		return screenB64, nil
	})

	// Search only in top-left quadrant — should not find the patch.
	region := &Rect{X: 0, Y: 0, Width: 100, Height: 100}
	result, err := matcher.FindByImage(refB64, region)
	if err != nil {
		t.Fatalf("FindByImage error: %v", err)
	}
	if result.Found {
		t.Errorf("expected Found=false when patch is outside search region")
	}
}


// mockOCR implements browser.OCRProvider for testing.
type mockOCR struct {
	results []browser.OCRResult
}

func (m *mockOCR) Recognize(pngBase64 string) ([]browser.OCRResult, error) {
	return m.results, nil
}
func (m *mockOCR) IsAvailable() bool { return true }
func (m *mockOCR) Close()            {}

func TestFindByText_Found(t *testing.T) {
	ocr := &mockOCR{
		results: []browser.OCRResult{
			{Text: "Cancel", Confidence: 0.95, BBox: [4]int{10, 20, 60, 30}},
			{Text: "OK Button", Confidence: 0.90, BBox: [4]int{100, 200, 80, 40}},
		},
	}

	screen := solidImage(300, 300, color.RGBA{200, 200, 200, 255})
	screenB64 := encodePNG(screen)

	matcher := NewImageMatcher(ocr, func() (string, error) {
		return screenB64, nil
	})

	result, err := matcher.FindByText("OK", nil)
	if err != nil {
		t.Fatalf("FindByText error: %v", err)
	}
	if !result.Found {
		t.Fatal("expected Found=true")
	}
	// Center of BBox [100, 200, 80, 40] = (140, 220).
	if result.X != 140 || result.Y != 220 {
		t.Errorf("expected center (140,220), got (%d,%d)", result.X, result.Y)
	}
}

func TestFindByText_NotFound(t *testing.T) {
	ocr := &mockOCR{
		results: []browser.OCRResult{
			{Text: "Cancel", Confidence: 0.95, BBox: [4]int{10, 20, 60, 30}},
		},
	}

	screen := solidImage(100, 100, color.RGBA{200, 200, 200, 255})
	screenB64 := encodePNG(screen)

	matcher := NewImageMatcher(ocr, func() (string, error) {
		return screenB64, nil
	})

	result, err := matcher.FindByText("Submit", nil)
	if err != nil {
		t.Fatalf("FindByText error: %v", err)
	}
	if result.Found {
		t.Error("expected Found=false for non-existent text")
	}
}

func TestFindByText_WithSearchRegion(t *testing.T) {
	ocr := &mockOCR{
		results: []browser.OCRResult{
			{Text: "OK", Confidence: 0.90, BBox: [4]int{10, 10, 40, 20}},   // center (30, 20) — inside region
			{Text: "OK", Confidence: 0.95, BBox: [4]int{200, 200, 40, 20}}, // center (220, 210) — outside region
		},
	}

	screen := solidImage(300, 300, color.RGBA{200, 200, 200, 255})
	screenB64 := encodePNG(screen)

	matcher := NewImageMatcher(ocr, func() (string, error) {
		return screenB64, nil
	})

	region := &Rect{X: 0, Y: 0, Width: 100, Height: 100}
	result, err := matcher.FindByText("OK", region)
	if err != nil {
		t.Fatalf("FindByText error: %v", err)
	}
	if !result.Found {
		t.Fatal("expected Found=true for text inside search region")
	}
	// Should match the first result (center 30, 20), not the second.
	if result.X != 30 || result.Y != 20 {
		t.Errorf("expected center (30,20), got (%d,%d)", result.X, result.Y)
	}
}

func TestFindByText_NilOCR(t *testing.T) {
	matcher := NewImageMatcher(nil, func() (string, error) {
		return "", nil
	})

	_, err := matcher.FindByText("anything", nil)
	if err == nil {
		t.Error("expected error when OCR provider is nil")
	}
}

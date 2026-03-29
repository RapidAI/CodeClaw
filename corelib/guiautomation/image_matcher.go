package guiautomation

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// MatchResult describes where a reference image was found.
type MatchResult struct {
	Found      bool    `json:"found"`
	X          int     `json:"x"`
	Y          int     `json:"y"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Confidence float64 `json:"confidence"`
}

// Rect represents a bounding rectangle used to limit search regions.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ImageMatcher locates UI elements by image comparison or OCR text.
type ImageMatcher struct {
	ocr          browser.OCRProvider
	screenshotFn func() (string, error) // returns base64 PNG
}

// NewImageMatcher creates an ImageMatcher.
func NewImageMatcher(ocr browser.OCRProvider, screenshotFn func() (string, error)) *ImageMatcher {
	return &ImageMatcher{ocr: ocr, screenshotFn: screenshotFn}
}

// confidenceThreshold is the minimum NCC score to consider a match valid.
const confidenceThreshold = 0.6

// FindByImage searches for a reference image snippet in the current screen
// using sliding window + Normalized Cross-Correlation (NCC).
// Both refImageB64 and the screenshot are expected to be base64-encoded PNG.
func (m *ImageMatcher) FindByImage(refImageB64 string, searchRegion *Rect) (*MatchResult, error) {
	// Take a screenshot of the current screen.
	screenB64, err := m.screenshotFn()
	if err != nil {
		return nil, fmt.Errorf("screenshot failed: %w", err)
	}

	screenImg, err := decodePNGBase64(screenB64)
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}

	refImg, err := decodePNGBase64(refImageB64)
	if err != nil {
		return nil, fmt.Errorf("decode reference image: %w", err)
	}

	refBounds := refImg.Bounds()
	refW := refBounds.Dx()
	refH := refBounds.Dy()
	if refW == 0 || refH == 0 {
		return &MatchResult{Found: false}, nil
	}

	// Determine the search area within the screenshot.
	screenBounds := screenImg.Bounds()
	searchMinX := screenBounds.Min.X
	searchMinY := screenBounds.Min.Y
	searchMaxX := screenBounds.Max.X
	searchMaxY := screenBounds.Max.Y

	if searchRegion != nil {
		searchMinX = max(searchMinX, searchRegion.X)
		searchMinY = max(searchMinY, searchRegion.Y)
		searchMaxX = min(searchMaxX, searchRegion.X+searchRegion.Width)
		searchMaxY = min(searchMaxY, searchRegion.Y+searchRegion.Height)
	}

	// The sliding window needs room for the reference image.
	maxX := searchMaxX - refW
	maxY := searchMaxY - refH
	if maxX < searchMinX || maxY < searchMinY {
		return &MatchResult{Found: false}, nil
	}

	bestScore := -1.0
	bestX, bestY := 0, 0

	// Sliding window with optional step for large images.
	step := 1
	totalPixels := (maxX - searchMinX) * (maxY - searchMinY)
	if totalPixels > 500*500 {
		step = 2 // coarse pass for large search areas
	}

	for sy := searchMinY; sy <= maxY; sy += step {
		for sx := searchMinX; sx <= maxX; sx += step {
			score := nccScore(screenImg, refImg, sx, sy, refW, refH)
			if score > bestScore {
				bestScore = score
				bestX = sx
				bestY = sy
			}
		}
	}

	// Refine around best position if we used a coarse step.
	if step > 1 && bestScore > 0.3 {
		fineMinX := max(searchMinX, bestX-step)
		fineMinY := max(searchMinY, bestY-step)
		fineMaxX := min(maxX, bestX+step)
		fineMaxY := min(maxY, bestY+step)
		for sy := fineMinY; sy <= fineMaxY; sy++ {
			for sx := fineMinX; sx <= fineMaxX; sx++ {
				score := nccScore(screenImg, refImg, sx, sy, refW, refH)
				if score > bestScore {
					bestScore = score
					bestX = sx
					bestY = sy
				}
			}
		}
	}

	if bestScore < confidenceThreshold {
		return &MatchResult{Found: false, Confidence: bestScore}, nil
	}

	return &MatchResult{
		Found:      true,
		X:          bestX + refW/2,
		Y:          bestY + refH/2,
		Width:      refW,
		Height:     refH,
		Confidence: bestScore,
	}, nil
}

// FindByText uses OCR to locate text on screen.
// It takes a screenshot, runs OCR, and finds the region containing targetText.
func (m *ImageMatcher) FindByText(targetText string, searchRegion *Rect) (*MatchResult, error) {
	if m.ocr == nil || !m.ocr.IsAvailable() {
		return nil, fmt.Errorf("OCR provider not available")
	}

	screenB64, err := m.screenshotFn()
	if err != nil {
		return nil, fmt.Errorf("screenshot failed: %w", err)
	}

	results, err := m.ocr.Recognize(screenB64)
	if err != nil {
		return nil, fmt.Errorf("OCR recognize failed: %w", err)
	}

	for _, r := range results {
		if !strings.Contains(r.Text, targetText) {
			continue
		}

		// BBox is [x, y, width, height].
		bx, by, bw, bh := r.BBox[0], r.BBox[1], r.BBox[2], r.BBox[3]

		// If a search region is specified, skip results outside it.
		if searchRegion != nil {
			centerX := bx + bw/2
			centerY := by + bh/2
			if centerX < searchRegion.X || centerX >= searchRegion.X+searchRegion.Width ||
				centerY < searchRegion.Y || centerY >= searchRegion.Y+searchRegion.Height {
				continue
			}
		}

		return &MatchResult{
			Found:      true,
			X:          bx + bw/2,
			Y:          by + bh/2,
			Width:      bw,
			Height:     bh,
			Confidence: r.Confidence,
		}, nil
	}

	return &MatchResult{Found: false}, nil
}

// decodePNGBase64 decodes a base64-encoded PNG string into an image.Image.
func decodePNGBase64(b64 string) (image.Image, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("png decode: %w", err)
	}
	return img, nil
}

// nccScore computes the Normalized Cross-Correlation between the reference
// image and a window in the screen image starting at (ox, oy).
// Returns a value in [-1, 1] where 1 means perfect match.
func nccScore(screen, ref image.Image, ox, oy, w, h int) float64 {
	var sumS, sumR, sumSS, sumRR, sumSR float64
	n := float64(w * h * 3) // 3 channels: R, G, B

	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			sr, sg, sb, _ := screen.At(ox+dx, oy+dy).RGBA()
			rr, rg, rb, _ := ref.At(ref.Bounds().Min.X+dx, ref.Bounds().Min.Y+dy).RGBA()

			// Convert from 16-bit to 8-bit.
			sv := [3]float64{float64(sr >> 8), float64(sg >> 8), float64(sb >> 8)}
			rv := [3]float64{float64(rr >> 8), float64(rg >> 8), float64(rb >> 8)}

			for c := 0; c < 3; c++ {
				sumS += sv[c]
				sumR += rv[c]
				sumSS += sv[c] * sv[c]
				sumRR += rv[c] * rv[c]
				sumSR += sv[c] * rv[c]
			}
		}
	}

	// NCC = (n*sumSR - sumS*sumR) / sqrt((n*sumSS - sumS^2) * (n*sumRR - sumR^2))
	numerator := n*sumSR - sumS*sumR
	denomA := n*sumSS - sumS*sumS
	denomB := n*sumRR - sumR*sumR

	if denomA <= 0 || denomB <= 0 {
		// Flat region — if both are flat and equal, perfect match.
		if denomA <= 0 && denomB <= 0 {
			meanS := sumS / n
			meanR := sumR / n
			if math.Abs(meanS-meanR) < 1.0 {
				return 1.0
			}
		}
		return 0.0
	}

	return numerator / math.Sqrt(denomA*denomB)
}

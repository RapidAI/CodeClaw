package guiautomation

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
)

// LocateStrategy describes how an element was located.
type LocateStrategy string

const (
	StrategyAccessibility LocateStrategy = "accessibility"
	StrategyImage         LocateStrategy = "image"
	StrategyCoordinate    LocateStrategy = "coordinate"
)

// LocateResult is the result of element location.
type LocateResult struct {
	Strategy   LocateStrategy
	X, Y       int
	Element    *accessibility.Element
	Confidence float64
}

// ElementLocator tries multiple strategies to find a UI element.
type ElementLocator struct {
	bridge  accessibility.Bridge
	matcher *ImageMatcher
	logger  func(string)
}

// NewElementLocator creates an ElementLocator with the given dependencies.
func NewElementLocator(bridge accessibility.Bridge, matcher *ImageMatcher, logger func(string)) *ElementLocator {
	return &ElementLocator{
		bridge:  bridge,
		matcher: matcher,
		logger:  logger,
	}
}

// Locate attempts to find the target element using the three-tier strategy:
// 1. Accessibility Bridge (if step has AccessibilityID)
// 2. Image matching (if step has SnapshotRef)
// 3. Coordinate fallback
func (l *ElementLocator) Locate(step GUIRecordedStep) (*LocateResult, error) {
	// Tier 1: Accessibility
	if step.AccessibilityID != nil && l.bridge != nil {
		l.log("locator: trying accessibility strategy for role=%s name=%s", step.AccessibilityID.Role, step.AccessibilityID.Name)
		el, err := l.bridge.FindElement(step.WindowTitle, step.AccessibilityID.Role, step.AccessibilityID.Name)
		if err == nil && el != nil {
			cx := el.Bounds.X + el.Bounds.Width/2
			cy := el.Bounds.Y + el.Bounds.Height/2
			l.log("locator: accessibility hit at (%d, %d)", cx, cy)
			return &LocateResult{
				Strategy:   StrategyAccessibility,
				X:          cx,
				Y:          cy,
				Element:    el,
				Confidence: 1.0,
			}, nil
		}
		l.log("locator: accessibility miss: %v", err)
	}

	// Tier 2: Image matching
	if step.SnapshotRef != "" && l.matcher != nil {
		l.log("locator: trying image strategy with snapshot %s", step.SnapshotRef)
		// SnapshotRef may be a file path (relative to flow dir) or inline base64.
		// If it looks like a file path, we skip image matching here — the caller
		// should resolve it to base64 before calling Locate. For now, only attempt
		// image matching if the ref looks like base64 data (starts with "iVBOR").
		refData := step.SnapshotRef
		if len(refData) > 100 && strings.HasPrefix(refData, "iVBOR") {
			result, err := l.matcher.FindByImage(refData, nil)
			if err == nil && result != nil && result.Found {
				l.log("locator: image hit at (%d, %d) confidence=%.2f", result.X, result.Y, result.Confidence)
				return &LocateResult{
					Strategy:   StrategyImage,
					X:          result.X,
					Y:          result.Y,
					Confidence: result.Confidence,
				}, nil
			}
			l.log("locator: image miss: %v", err)
		} else {
			l.log("locator: snapshot ref is a file path, skipping image matching (not yet resolved to base64)")
		}
	}

	// Tier 3: Coordinate fallback
	x, y := step.Coords[0], step.Coords[1]
	if x == 0 && y == 0 {
		return nil, fmt.Errorf("all locate strategies failed and no valid coordinates recorded")
	}
	l.log("locator: falling back to coordinates (%d, %d)", x, y)
	return &LocateResult{
		Strategy:   StrategyCoordinate,
		X:          x,
		Y:          y,
		Confidence: 0.0,
	}, nil
}

func (l *ElementLocator) log(format string, args ...any) {
	if l.logger != nil {
		l.logger(fmt.Sprintf(format, args...))
	}
}

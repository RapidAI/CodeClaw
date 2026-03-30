package tool

import (
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"
)

// Feature: skillrouter-body-aware-retrieval, Property 1: TruncateBody 短文本恒等
// For any body with rune length ≤ maxChars, output equals input.
// **Validates: Requirements 1.4, 11.4**
func TestProperty_TruncateBodyIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxChars := rapid.IntRange(1, 3000).Draw(t, "maxChars")
		// Generate a body whose rune length is at most maxChars.
		body := rapid.StringOfN(rapid.Rune(), 0, maxChars, -1).Draw(t, "body")

		result := TruncateBody(body, maxChars)
		if result != body {
			t.Fatalf("expected identity for body rune len %d ≤ maxChars %d\n  input:  %q\n  output: %q",
				utf8.RuneCountInString(body), maxChars, body, result)
		}
	})
}

// Feature: skillrouter-body-aware-retrieval, Property 2: TruncateBody 输出不变量
// For any body and maxChars > 0:
//
//	(a) output rune length ≤ maxChars + len("...")
//	(b) every line in output (excluding trailing "...") is an exact copy of an input line
//	(c) output ends with "\n..." or "..." iff truncation occurred
//
// **Validates: Requirements 1.3, 1.6, 11.2, 11.3, 11.5**
func TestProperty_TruncateBodyInvariants(t *testing.T) {
	// Generator for markdown-like documents with headings, lists, code blocks.
	genMarkdownBody := rapid.Custom(func(t *rapid.T) string {
		numLines := rapid.IntRange(1, 30).Draw(t, "numLines")
		lines := make([]string, numLines)
		for i := range lines {
			kind := rapid.IntRange(0, 3).Draw(t, "lineKind")
			content := rapid.StringMatching(`[a-z0-9 ]{0,80}`).Draw(t, "lineContent")
			switch kind {
			case 0:
				lines[i] = "# " + content
			case 1:
				lines[i] = "- " + content
			case 2:
				lines[i] = "```" + content
			default:
				lines[i] = content
			}
		}
		return strings.Join(lines, "\n")
	})

	rapid.Check(t, func(t *rapid.T) {
		body := genMarkdownBody.Draw(t, "body")
		maxChars := rapid.IntRange(1, 5000).Draw(t, "maxChars")

		result := TruncateBody(body, maxChars)
		bodyRunes := utf8.RuneCountInString(body)
		resultRunes := utf8.RuneCountInString(result)
		truncated := bodyRunes > maxChars

		// (a) Output rune length bounded.
		// The suffix is "\n..." (4 runes) when complete lines are kept, or "..." (3 runes)
		// for the single overlong line case.
		maxAllowed := maxChars + utf8.RuneCountInString("\n...")
		if resultRunes > maxAllowed {
			t.Fatalf("output rune length %d exceeds maxChars(%d)+len('\\n...')=%d",
				resultRunes, maxChars, maxAllowed)
		}

		// (c) Ends with "..." iff truncation occurred.
		hasSuffix := strings.HasSuffix(result, "...")
		if truncated && !hasSuffix {
			t.Fatalf("truncation occurred but output does not end with '...'\n  body runes: %d, maxChars: %d\n  result: %q",
				bodyRunes, maxChars, result)
		}
		if !truncated && hasSuffix && body != result {
			t.Fatalf("no truncation but output ends with '...'\n  body runes: %d, maxChars: %d\n  result: %q",
				bodyRunes, maxChars, result)
		}

		// (b) Every line in output (excluding trailing "...") is an exact copy of an input line.
		if truncated {
			trimmed := result
			if strings.HasSuffix(trimmed, "\n...") {
				trimmed = strings.TrimSuffix(trimmed, "\n...")
			} else if strings.HasSuffix(trimmed, "...") {
				// Single overlong line case — skip line-by-line check.
				return
			}
			inputLines := make(map[string]bool)
			for _, l := range strings.Split(body, "\n") {
				inputLines[l] = true
			}
			for _, outLine := range strings.Split(trimmed, "\n") {
				if !inputLines[outLine] {
					t.Fatalf("output line %q not found in input lines", outLine)
				}
			}
		}
	})
}

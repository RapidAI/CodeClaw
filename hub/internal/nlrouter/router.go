package nlrouter

import (
	"context"
	"strings"
	"time"
)

// MemoryStore abstracts the memory package to avoid direct dependency.
type MemoryStore interface {
	GetDefaultTool(ctx context.Context, userID string) string
}

// Router is the natural language intent parser. It combines a rule engine,
// a memory store, and a context window manager to resolve user text into
// structured Intent objects.
type Router struct {
	rules   *RuleEngine
	memory  MemoryStore
	context *ContextWindowManager
}

// NewRouter creates a Router with the given dependencies.
func NewRouter(rules *RuleEngine, memory MemoryStore, ctxMgr *ContextWindowManager) *Router {
	return &Router{
		rules:   rules,
		memory:  memory,
		context: ctxMgr,
	}
}

// pronouns that should resolve to the last referenced entity.
var pronounTokens = []string{"它", "那个", "上一个", "this", "that", "the last one"}

// repeatTokens trigger replay of the last user intent.
var repeatTokens = []string{"再来一次", "重复", "repeat", "again", "do it again"}

// Parse resolves user text into an Intent.
//
// Flow:
//  1. Check for repeat requests ("再来一次", "again") → replay last intent.
//  2. Resolve pronouns ("它", "那个") from ContextWindow.
//  3. Run RuleEngine.Match on the (possibly rewritten) text.
//  4. If multiple candidates, pick highest confidence and attach candidates.
//  5. If no match or confidence < 0.6, fall back to send_input when user has
//     an active session, otherwise return unknown.
//  6. For launch_session with missing "tool" param, fill from MemoryStore.
//  7. Append the parsed entry to the ContextWindow.
func (r *Router) Parse(ctx context.Context, userID, text string) (*Intent, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return &Intent{
			Name:       IntentUnknown,
			Confidence: 0.0,
			Params:     make(map[string]interface{}),
			RawText:    text,
		}, nil
	}

	win := r.context.Get(userID)

	// 1. Repeat request — replay last user intent from ContextWindow.
	if isRepeatRequest(text) {
		if intent := r.replayLastIntent(win, text); intent != nil {
			r.recordEntry(userID, text, intent.Name)
			return intent, nil
		}
		// Nothing to repeat; fall through to normal parsing.
	}

	// 2. Pronoun resolution — rewrite text with the last referenced entity.
	resolved := r.resolvePronouns(text, win)

	// 3. Rule engine matching.
	intent := r.rules.Match(resolved)

	// 4. Confidence gate & active-session fallback.
	if intent == nil || intent.Confidence < 0.6 {
		if win.ActiveSession != "" {
			// User is inside a session context → default to send_input.
			intent = &Intent{
				Name:       IntentSendInput,
				Confidence: 0.7,
				Params:     map[string]interface{}{"text": text},
				RawText:    text,
			}
			r.recordEntry(userID, text, intent.Name)
			return intent, nil
		}
		// No session context → unknown.
		intent = &Intent{
			Name:       IntentUnknown,
			Confidence: 0.0,
			Params:     make(map[string]interface{}),
			RawText:    text,
		}
		r.recordEntry(userID, text, intent.Name)
		return intent, nil
	}

	// Keep original raw text even if we rewrote for pronoun resolution.
	intent.RawText = text

	// 5. Multi-intent: collect all matches and pick highest confidence.
	intent = r.selectBestIntent(resolved, intent)

	// 6. Fill missing "tool" param for launch_session from MemoryStore.
	if intent.Name == IntentLaunchSession {
		if _, ok := intent.Params["tool"]; !ok {
			if def := r.memory.GetDefaultTool(ctx, userID); def != "" {
				intent.Params["tool"] = def
			}
		}
	}

	// 7. Record to context window.
	r.recordEntry(userID, text, intent.Name)

	return intent, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// isRepeatRequest returns true when text is a repeat/replay trigger.
func isRepeatRequest(text string) bool {
	lower := strings.ToLower(text)
	for _, tok := range repeatTokens {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

// replayLastIntent finds the most recent user-role entry with a non-unknown
// intent in the ContextWindow and returns a copy of that intent.
func (r *Router) replayLastIntent(win *ContextWindow, rawText string) *Intent {
	for i := len(win.Entries) - 1; i >= 0; i-- {
		e := win.Entries[i]
		if e.Role == "user" && e.Intent != "" && e.Intent != IntentUnknown {
			// Re-parse the original text to reconstruct params.
			replayed := r.rules.Match(e.Text)
			if replayed != nil {
				replayed.RawText = rawText
				return replayed
			}
			// If rule engine can't re-parse, build a minimal intent.
			return &Intent{
				Name:       e.Intent,
				Confidence: 0.9,
				Params:     make(map[string]interface{}),
				RawText:    rawText,
			}
		}
	}
	return nil
}

// resolvePronouns replaces pronoun tokens in text with the last referenced
// entity found in the ContextWindow (ActiveTool, ActiveSession, or last
// entry text).
func (r *Router) resolvePronouns(text string, win *ContextWindow) string {
	lower := strings.ToLower(text)
	for _, p := range pronounTokens {
		if !strings.Contains(lower, p) {
			continue
		}
		ref := r.findLastReference(win)
		if ref == "" {
			break
		}
		// Replace the first occurrence of the pronoun with the reference.
		text = replaceCaseInsensitive(text, p, ref)
		break // resolve only the first pronoun found
	}
	return text
}

// findLastReference returns the most recent concrete entity from the context
// window: ActiveTool, ActiveSession, or the text of the last user entry.
func (r *Router) findLastReference(win *ContextWindow) string {
	if win.ActiveTool != "" {
		return win.ActiveTool
	}
	if win.ActiveSession != "" {
		return win.ActiveSession
	}
	// Fall back to the last user entry's text.
	for i := len(win.Entries) - 1; i >= 0; i-- {
		if win.Entries[i].Role == "user" && win.Entries[i].Text != "" {
			return win.Entries[i].Text
		}
	}
	return ""
}

// selectBestIntent collects all rule-engine matches (slash, keyword, fuzzy)
// and returns the one with the highest confidence, attaching others as
// Candidates.
func (r *Router) selectBestIntent(text string, primary *Intent) *Intent {
	// The RuleEngine already returns the single best match per tier.
	// We do a second pass to see if a lower-tier also matches, and if so
	// attach it as a candidate.
	candidates := r.collectCandidates(text, primary)
	if len(candidates) > 0 {
		primary.Candidates = candidates
	}
	return primary
}

// collectCandidates gathers alternative intents that differ from primary.
func (r *Router) collectCandidates(text string, primary *Intent) []Intent {
	var candidates []Intent
	lower := strings.ToLower(text)

	// Check keyword rules for any additional matches.
	for _, rule := range r.rules.keywordRules {
		for _, kw := range rule.keywords {
			if strings.Contains(lower, kw) && rule.intent != primary.Name {
				candidates = append(candidates, Intent{
					Name:       rule.intent,
					Confidence: 0.8,
					Params:     make(map[string]interface{}),
					RawText:    text,
				})
				break // one candidate per intent
			}
		}
	}

	// Deduplicate by intent name.
	seen := map[string]bool{primary.Name: true}
	var deduped []Intent
	for _, c := range candidates {
		if !seen[c.Name] {
			seen[c.Name] = true
			deduped = append(deduped, c)
		}
	}
	return deduped
}

// recordEntry appends a user entry to the ContextWindow.
func (r *Router) recordEntry(userID, text, intentName string) {
	r.context.Add(userID, ContextEntry{
		Role:      "user",
		Text:      text,
		Intent:    intentName,
		Timestamp: time.Now(),
	})
}

// replaceCaseInsensitive replaces the first case-insensitive occurrence of
// old in s with new_.
func replaceCaseInsensitive(s, old, new_ string) string {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, strings.ToLower(old))
	if idx < 0 {
		return s
	}
	return s[:idx] + new_ + s[idx+len(old):]
}

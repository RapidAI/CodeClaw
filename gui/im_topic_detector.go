package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// TopicDecision is the result of topic switch detection.
type TopicDecision int

const (
	// TopicSame means the new message continues the current conversation topic.
	TopicSame TopicDecision = iota
	// TopicNew means the new message starts a new topic; context should be cleared.
	TopicNew
)

// topicSwitchDetector detects when a user's new message is about a different
// topic than the current conversation, enabling automatic context clearing.
// It uses a two-stage approach: fast BM25 scoring first, then a short LLM
// call only when the BM25 score falls in the ambiguous zone.
type topicSwitchDetector struct {
	// bm25SameThreshold: above this → definitely same topic (skip LLM).
	bm25SameThreshold float64
	// bm25NewThreshold: below this → definitely new topic (skip LLM).
	bm25NewThreshold float64
	// timeDecayMinutes: idle time after which decay starts. Score decays
	// linearly to zero over 2× this duration.
	timeDecayMinutes float64
	// minTurnsForDetection: don't run detection if fewer than this many
	// user turns exist (too little context to judge).
	minTurnsForDetection int
	// llmTimeout is the maximum time to wait for the LLM confirmation call.
	llmTimeout time.Duration

	llmClient func() (*http.Client, MaclawLLMConfig)
}

func newTopicSwitchDetector(llmClient func() (*http.Client, MaclawLLMConfig)) *topicSwitchDetector {
	return &topicSwitchDetector{
		bm25SameThreshold:    1.0,
		bm25NewThreshold:     0.3,
		timeDecayMinutes:     30,
		minTurnsForDetection: 3,
		llmTimeout:           5 * time.Second,
		llmClient:            llmClient,
	}
}

// detect checks whether newMessage is a continuation of the user's current
// conversation or a new topic. Returns TopicNew if context should be cleared.
func (d *topicSwitchDetector) detect(newMessage string, userID string, mem *conversationMemory) TopicDecision {
	entries := mem.load(userID)
	if len(entries) == 0 {
		return TopicSame // first message, nothing to clear
	}

	// Collect recent user messages as context.
	var userTexts []string
	for _, e := range entries {
		if e.Role == "user" {
			if text, ok := e.Content.(string); ok && text != "" {
				userTexts = append(userTexts, text)
			}
		}
	}
	if len(userTexts) < d.minTurnsForDetection {
		return TopicSame // too few turns to judge
	}

	// Take last 5 user messages as context.
	if len(userTexts) > 5 {
		userTexts = userTexts[len(userTexts)-5:]
	}
	contextText := strings.Join(userTexts, "\n")

	// Stage 1: BM25 scoring.
	idx := bm25.New()
	idx.Rebuild([]bm25.Doc{{ID: "ctx", Text: contextText}})
	scores := idx.Score(newMessage)
	rawScore := scores["ctx"] // 0 if not present

	// Apply time decay: score stays full until timeDecayMinutes, then
	// decays linearly to zero over another timeDecayMinutes window.
	// e.g. with 30min setting: 0-30min → decay=1.0, 30-60min → linear
	// decay, >60min → decay=0.
	lastAccess := mem.lastAccessTime(userID)
	decay := 1.0
	if !lastAccess.IsZero() && d.timeDecayMinutes > 0 {
		elapsed := time.Since(lastAccess).Minutes()
		if elapsed > d.timeDecayMinutes {
			excess := elapsed - d.timeDecayMinutes
			decay = 1.0 - excess/d.timeDecayMinutes
			if decay < 0 {
				decay = 0
			}
		}
	}
	adjustedScore := rawScore * decay

	if adjustedScore >= d.bm25SameThreshold {
		return TopicSame
	}
	if adjustedScore <= d.bm25NewThreshold {
		log.Printf("[TopicDetector] auto-clear: bm25=%.2f decay=%.2f adjusted=%.2f → new topic", rawScore, decay, adjustedScore)
		return TopicNew
	}

	// Stage 2: LLM confirmation for ambiguous zone.
	if d.llmClient == nil {
		return TopicSame // conservative fallback
	}
	decision := d.confirmWithLLM(contextText, newMessage)
	log.Printf("[TopicDetector] llm-confirm: bm25=%.2f adjusted=%.2f → %v", rawScore, adjustedScore, decision)
	return decision
}

// confirmWithLLM makes a very short LLM call (~50-100 tokens) to determine
// if the new message is a topic switch. Returns TopicSame on any error.
func (d *topicSwitchDetector) confirmWithLLM(contextText, newMessage string) TopicDecision {
	httpClient, cfg := d.llmClient()
	if cfg.URL == "" || cfg.Model == "" {
		return TopicSame
	}

	// Truncate by runes (not bytes) to avoid cutting multi-byte UTF-8 chars.
	contextText = truncateRunes(contextText, 200)
	newMessage = truncateRunes(newMessage, 200)

	messages := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": "判断用户的新消息是否延续之前的对话话题。只回答 same 或 new，不要解释。",
		},
		map[string]interface{}{
			"role":    "user",
			"content": fmt.Sprintf("之前的话题:\n%s\n\n新消息:\n%s", contextText, newMessage),
		},
	}

	reqBody := map[string]interface{}{
		"model":      cfg.Model,
		"messages":   messages,
		"max_tokens": 10,
	}
	data, _ := json.Marshal(reqBody)

	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"

	ctx, cancel := context.WithTimeout(context.Background(), d.llmTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return TopicSame
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	client := httpClient
	if client == nil {
		client = &http.Client{Timeout: d.llmTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return TopicSame
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return TopicSame
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Choices) == 0 {
		return TopicSame
	}

	answer := strings.TrimSpace(strings.ToLower(result.Choices[0].Message.Content))
	if strings.Contains(answer, "new") {
		return TopicNew
	}
	return TopicSame
}

// truncateRunes truncates a string to at most n runes, preserving
// multi-byte UTF-8 characters.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// buildQuickSummary creates a one-line summary from conversation entries
// for archival before auto-clearing. Takes the last user message as the
// most representative topic indicator.
func buildQuickSummary(entries []conversationEntry) string {
	var lastUserText string
	for _, e := range entries {
		if e.Role == "user" {
			if text, ok := e.Content.(string); ok && text != "" {
				lastUserText = text
			}
		}
	}
	if lastUserText == "" {
		return ""
	}
	runes := []rune(lastUserText)
	if len(runes) > 100 {
		lastUserText = string(runes[:100]) + "..."
	}
	return "对话话题: " + lastUserText
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// HubSkillMeta is the client-side Skill metadata returned from SkillHub searches.
// It includes a HubURL field to track which Hub the result came from.
type HubSkillMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	TrustLevel  string   `json:"trust_level"`
	Downloads   int      `json:"downloads"`
	HubURL      string   `json:"hub_url"`
}

// cachedSearchResult holds a cached search response with expiry.
type cachedSearchResult struct {
	results   []HubSkillMeta
	expiresAt time.Time
}

// hubSearchResponse is the JSON structure returned by Hub search endpoints.
type hubSearchResponse struct {
	Skills []HubSkillMeta `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}

// hubDownloadResponse is the JSON structure returned by Hub download endpoints.
type hubDownloadResponse struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Version     string                   `json:"version"`
	Author      string                   `json:"author"`
	TrustLevel  string                   `json:"trust_level"`
	Triggers    []string                 `json:"triggers"`
	Steps       []map[string]interface{} `json:"steps"`
}

const maxCacheEntries = 100 // prevent unbounded cache growth

// SkillHubClient queries multiple SkillHubs concurrently, caches results,
// and downloads/installs Skills.
type SkillHubClient struct {
	app      *App
	mu       sync.RWMutex
	cache    map[string]cachedSearchResult
	cacheTTL time.Duration
	recIndex []HubSkillMeta
	client   *http.Client
}

// NewSkillHubClient creates a new SkillHubClient with default settings.
func NewSkillHubClient(app *App) *SkillHubClient {
	return &SkillHubClient{
		app:      app,
		cache:    make(map[string]cachedSearchResult),
		cacheTTL: 5 * time.Minute,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Search queries all configured Hubs concurrently and returns deduplicated results.
// Returns an empty slice (not an error) when all Hubs are unreachable.
func (c *SkillHubClient) Search(ctx context.Context, query string) ([]HubSkillMeta, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Check cache first.
	c.mu.RLock()
	if cached, ok := c.cache[query]; ok && time.Now().Before(cached.expiresAt) {
		results := cached.results
		c.mu.RUnlock()
		return results, nil
	}
	c.mu.RUnlock()

	// Load Hub URLs from config.
	cfg, err := c.app.LoadConfig()
	if err != nil || len(cfg.SkillHubURLs) == 0 {
		return nil, nil
	}

	type hubResult struct {
		hubURL  string
		skills  []HubSkillMeta
		latency int64
	}

	var wg sync.WaitGroup
	resultsCh := make(chan hubResult, len(cfg.SkillHubURLs))

	for _, entry := range cfg.SkillHubURLs {
		wg.Add(1)
		go func(hubURL string) {
			defer wg.Done()
			hubCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/skills/search?q=" + url.QueryEscape(query)
			req, reqErr := http.NewRequestWithContext(hubCtx, http.MethodGet, endpoint, nil)
			if reqErr != nil {
				return
			}
			req.Header.Set("User-Agent", "MaClaw/1.0")

			start := time.Now()
			resp, doErr := c.client.Do(req)
			latency := time.Since(start).Milliseconds()
			if doErr != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return
			}

			var sr hubSearchResponse
			if decErr := json.NewDecoder(resp.Body).Decode(&sr); decErr != nil {
				return
			}

			// Tag each skill with the source Hub URL.
			for i := range sr.Skills {
				sr.Skills[i].HubURL = hubURL
			}

			resultsCh <- hubResult{hubURL: hubURL, skills: sr.Skills, latency: latency}
		}(entry.URL)
	}

	wg.Wait()
	close(resultsCh)

	// Collect results per hub.
	allResults := make(map[string][]hubSearchResponse)
	latencies := make(map[string]int64)
	for hr := range resultsCh {
		allResults[hr.hubURL] = append(allResults[hr.hubURL], hubSearchResponse{Skills: hr.skills})
		latencies[hr.hubURL] = hr.latency
	}

	merged := mergeResults(allResults, latencies)

	// Cache the result (evict oldest entries if cache is full).
	c.mu.Lock()
	if len(c.cache) >= maxCacheEntries {
		// Evict expired entries first.
		now := time.Now()
		for k, v := range c.cache {
			if now.After(v.expiresAt) {
				delete(c.cache, k)
			}
		}
		// If still full, evict the entry closest to expiry.
		if len(c.cache) >= maxCacheEntries {
			var oldestKey string
			var oldestTime time.Time
			for k, v := range c.cache {
				if oldestKey == "" || v.expiresAt.Before(oldestTime) {
					oldestKey = k
					oldestTime = v.expiresAt
				}
			}
			delete(c.cache, oldestKey)
		}
	}
	c.cache[query] = cachedSearchResult{
		results:   merged,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
	c.mu.Unlock()

	return merged, nil
}

// Install downloads a Skill from the specified Hub and converts it to an NLSkillEntry.
// On failure it falls back to other Hubs sorted by latency.
func (c *SkillHubClient) Install(ctx context.Context, skillID string, hubURL string) (*NLSkillEntry, error) {
	// Try the specified Hub first, then fall back to others.
	hubURLs := []string{hubURL}
	fallbacks := c.selectBestHub(skillID)
	for _, u := range fallbacks {
		if u != hubURL {
			hubURLs = append(hubURLs, u)
		}
	}

	var lastErr error
	for _, hub := range hubURLs {
		entry, err := c.downloadSkill(ctx, skillID, hub)
		if err != nil {
			lastErr = err
			continue
		}
		return entry, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to install skill %s: %w", skillID, lastErr)
	}
	return nil, fmt.Errorf("no hubs available to install skill %s", skillID)
}

// CheckUpdate checks whether a Hub Skill has a newer version available.
// It queries all configured Hubs concurrently (8-second timeout per Hub),
// returning the first result where the Hub version differs from currentVersion.
// Returns nil, nil if versions match or no Hub is reachable.
func (c *SkillHubClient) CheckUpdate(ctx context.Context, skillID string, currentVersion string) (*HubSkillMeta, error) {
	cfg, err := c.app.LoadConfig()
	if err != nil || len(cfg.SkillHubURLs) == 0 {
		return nil, nil
	}

	type checkResult struct {
		meta *HubSkillMeta
	}

	resultsCh := make(chan checkResult, len(cfg.SkillHubURLs))
	var wg sync.WaitGroup

	for _, entry := range cfg.SkillHubURLs {
		wg.Add(1)
		go func(hubURL string) {
			defer wg.Done()
			hubCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/skills/" + url.PathEscape(skillID)
			req, reqErr := http.NewRequestWithContext(hubCtx, http.MethodGet, endpoint, nil)
			if reqErr != nil {
				return
			}
			req.Header.Set("User-Agent", "MaClaw/1.0")

			resp, doErr := c.client.Do(req)
			if doErr != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return
			}

			var meta HubSkillMeta
			if decErr := json.NewDecoder(resp.Body).Decode(&meta); decErr != nil {
				return
			}
			meta.HubURL = hubURL
			resultsCh <- checkResult{meta: &meta}
		}(entry.URL)
	}

	wg.Wait()
	close(resultsCh)

	for cr := range resultsCh {
		if cr.meta != nil && cr.meta.Version != currentVersion {
			return cr.meta, nil
		}
	}
	return nil, nil
}

// downloadSkill fetches a single Skill from a Hub and converts it to NLSkillEntry.
func (c *SkillHubClient) downloadSkill(ctx context.Context, skillID string, hubURL string) (*NLSkillEntry, error) {
	dlCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/skills/" + url.PathEscape(skillID) + "/download"
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub %s returned HTTP %d for skill %s", hubURL, resp.StatusCode, skillID)
	}

	var dl hubDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&dl); err != nil {
		return nil, fmt.Errorf("failed to decode skill response: %w", err)
	}

	// Convert steps from generic maps to NLSkillStep.
	steps := make([]NLSkillStep, 0, len(dl.Steps))
	for _, raw := range dl.Steps {
		step := NLSkillStep{}
		if action, ok := raw["action"].(string); ok {
			step.Action = action
		}
		if params, ok := raw["params"].(map[string]interface{}); ok {
			step.Params = params
		}
		if onErr, ok := raw["on_error"].(string); ok {
			step.OnError = onErr
		}
		steps = append(steps, step)
	}

	entry := &NLSkillEntry{
		Name:          dl.Name,
		Description:   dl.Description,
		Triggers:      dl.Triggers,
		Steps:         steps,
		Status:        "active",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Source:        "hub",
		SourceProject: hubURL,
		HubSkillID:    dl.ID,
		HubVersion:    dl.Version,
		TrustLevel:    dl.TrustLevel,
	}

	return entry, nil
}

// selectBestHub returns Hub URLs sorted by latency (lowest first) using PingSkillHub data.
// Pings are performed concurrently for better performance with multiple hubs.
func (c *SkillHubClient) selectBestHub(skillID string) []string {
	cfg, err := c.app.LoadConfig()
	if err != nil || len(cfg.SkillHubURLs) == 0 {
		return nil
	}

	type hubLatency struct {
		url     string
		latency int64
	}

	results := make([]hubLatency, len(cfg.SkillHubURLs))
	var wg sync.WaitGroup

	for i, entry := range cfg.SkillHubURLs {
		wg.Add(1)
		go func(idx int, hubURL string) {
			defer wg.Done()
			result := c.app.PingSkillHub(hubURL)
			online, _ := result["online"].(bool)
			var ms int64
			switch v := result["ms"].(type) {
			case int64:
				ms = v
			case int:
				ms = int64(v)
			}
			if !online {
				ms = 999999
			}
			results[idx] = hubLatency{url: hubURL, latency: ms}
		}(i, entry.URL)
	}

	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		return results[i].latency < results[j].latency
	})

	urls := make([]string, len(results))
	for i, e := range results {
		urls[i] = e.url
	}
	return urls
}

// mergeResults deduplicates skills from multiple Hubs by Skill ID,
// keeping the result from the Hub with the lowest latency.
func mergeResults(results map[string][]hubSearchResponse, latencies map[string]int64) []HubSkillMeta {
	// bestByID tracks the best (lowest latency) skill per ID.
	type bestEntry struct {
		skill   HubSkillMeta
		latency int64
	}
	bestByID := make(map[string]bestEntry)

	for hubURL, responses := range results {
		lat := latencies[hubURL]
		for _, resp := range responses {
			for _, sk := range resp.Skills {
				existing, found := bestByID[sk.ID]
				if !found || lat < existing.latency {
					bestByID[sk.ID] = bestEntry{skill: sk, latency: lat}
				}
			}
		}
	}

	merged := make([]HubSkillMeta, 0, len(bestByID))
	for _, entry := range bestByID {
		merged = append(merged, entry.skill)
	}
	return merged
}

// RefreshRecommendations fetches popular Skills from all configured Hubs
// and merges them into the in-memory recommendation index.
// Errors from individual Hubs are silently ignored (best-effort).
func (c *SkillHubClient) RefreshRecommendations(ctx context.Context) error {
	cfg, err := c.app.LoadConfig()
	if err != nil || len(cfg.SkillHubURLs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	type hubPopularResult struct {
		skills []HubSkillMeta
	}
	resultsCh := make(chan hubPopularResult, len(cfg.SkillHubURLs))

	for _, entry := range cfg.SkillHubURLs {
		wg.Add(1)
		go func(hubURL string) {
			defer wg.Done()
			hubCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/skills/popular"
			req, reqErr := http.NewRequestWithContext(hubCtx, http.MethodGet, endpoint, nil)
			if reqErr != nil {
				return
			}
			req.Header.Set("User-Agent", "MaClaw/1.0")

			resp, doErr := c.client.Do(req)
			if doErr != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return
			}

			var skills []HubSkillMeta
			if decErr := json.NewDecoder(resp.Body).Decode(&skills); decErr != nil {
				return
			}

			for i := range skills {
				skills[i].HubURL = hubURL
			}

			resultsCh <- hubPopularResult{skills: skills}
		}(entry.URL)
	}

	wg.Wait()
	close(resultsCh)

	// Deduplicate by Skill ID, keeping the first occurrence.
	seen := make(map[string]struct{})
	var merged []HubSkillMeta
	for hr := range resultsCh {
		for _, sk := range hr.skills {
			if _, exists := seen[sk.ID]; !exists {
				seen[sk.ID] = struct{}{}
				merged = append(merged, sk)
			}
		}
	}

	c.mu.Lock()
	c.recIndex = merged
	c.mu.Unlock()

	return nil
}

// GetRecommendations returns the locally cached recommendation list (thread-safe).
func (c *SkillHubClient) GetRecommendations() []HubSkillMeta {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]HubSkillMeta, len(c.recIndex))
	copy(result, c.recIndex)
	return result
}

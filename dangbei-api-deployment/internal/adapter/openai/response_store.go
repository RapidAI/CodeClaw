package openai

import (
	"sync"
	"time"

	"ds2api/internal/auth"
)

type storedResponse struct {
	Owner     string
	Value     map[string]any
	ExpiresAt time.Time
}

type responseStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]storedResponse
}

func newResponseStore(ttl time.Duration) *responseStore {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &responseStore{
		ttl:   ttl,
		items: make(map[string]storedResponse),
	}
}

func responseStoreKey(owner, id string) string {
	return owner + "\x00" + id
}

func responseStoreOwner(a *auth.RequestAuth) string {
	if a == nil {
		return ""
	}
	return a.CallerID
}

func (s *responseStore) put(owner, id string, value map[string]any) {
	if s == nil || owner == "" || id == "" || value == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	s.items[responseStoreKey(owner, id)] = storedResponse{
		Owner:     owner,
		Value:     cloneAnyMap(value),
		ExpiresAt: now.Add(s.ttl),
	}
}

func (s *responseStore) get(owner, id string) (map[string]any, bool) {
	if s == nil || owner == "" || id == "" {
		return nil, false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	item, ok := s.items[responseStoreKey(owner, id)]
	if !ok {
		return nil, false
	}
	if item.Owner != owner {
		return nil, false
	}
	return cloneAnyMap(item.Value), true
}

func (s *responseStore) sweepLocked(now time.Time) {
	for k, v := range s.items {
		if now.After(v.ExpiresAt) {
			delete(s.items, k)
		}
	}
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (h *Handler) getResponseStore() *responseStore {
	if h == nil {
		return nil
	}
	h.responsesMu.Lock()
	defer h.responsesMu.Unlock()
	if h.responses == nil {
		ttl := 15 * time.Minute
		if h.Store != nil {
			ttl = time.Duration(h.Store.ResponsesStoreTTLSeconds()) * time.Second
		}
		h.responses = newResponseStore(ttl)
	}
	return h.responses
}

package im

import (
	"context"
	"log"
	"sync"
	"time"
)

// SmartRouteStore is the minimal interface needed to check smart route permission.
type SmartRouteStore interface {
	// GetSmartRouteByUserID returns the smart_route flag for a user.
	GetSmartRouteByUserID(ctx context.Context, userID string) (bool, error)
}

// SmartRouteSettingsReader reads the global smart_route_all setting.
type SmartRouteSettingsReader interface {
	Get(ctx context.Context, key string) (string, error)
}

// dbSmartRouteChecker checks smart route permission against the database.
// It caches the global "smart_route_all" flag for 30 seconds to avoid
// hitting the DB on every message.
type dbSmartRouteChecker struct {
	users    SmartRouteStore
	settings SmartRouteSettingsReader

	mu          sync.RWMutex
	globalCache bool
	cacheTime   time.Time
}

const smartRouteGlobalCacheTTL = 30 * time.Second

// NewDBSmartRouteChecker creates a SmartRouteChecker backed by the database.
func NewDBSmartRouteChecker(users SmartRouteStore, settings SmartRouteSettingsReader) SmartRouteChecker {
	return &dbSmartRouteChecker{users: users, settings: settings}
}

func (c *dbSmartRouteChecker) IsSmartRouteEnabled(ctx context.Context, userID string) bool {
	// Check global toggle first (cached).
	if c.isGlobalEnabled(ctx) {
		return true
	}
	// Fall back to per-user flag.
	enabled, err := c.users.GetSmartRouteByUserID(ctx, userID)
	if err != nil {
		log.Printf("[SmartRouteChecker] user lookup error for %s: %v", userID, err)
		return false // fail closed
	}
	return enabled
}

func (c *dbSmartRouteChecker) isGlobalEnabled(ctx context.Context) bool {
	c.mu.RLock()
	if time.Since(c.cacheTime) < smartRouteGlobalCacheTTL {
		v := c.globalCache
		c.mu.RUnlock()
		return v
	}
	c.mu.RUnlock()

	// Refresh cache.
	raw, _ := c.settings.Get(ctx, "smart_route_all")
	enabled := raw == "true"

	c.mu.Lock()
	c.globalCache = enabled
	c.cacheTime = time.Now()
	c.mu.Unlock()
	return enabled
}

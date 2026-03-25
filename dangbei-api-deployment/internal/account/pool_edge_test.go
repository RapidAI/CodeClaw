package account

import (
	"context"
	"sync"
	"testing"
	"time"

	"ds2api/internal/config"
)

// ─── Pool edge cases ─────────────────────────────────────────────────

func TestPoolEmptyNoAccounts(t *testing.T) {
	t.Setenv("DS2API_ACCOUNT_MAX_INFLIGHT", "2")
	t.Setenv("DS2API_ACCOUNT_CONCURRENCY", "")
	t.Setenv("DS2API_ACCOUNT_MAX_QUEUE", "")
	t.Setenv("DS2API_ACCOUNT_QUEUE_SIZE", "")
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[]}`)
	pool := NewPool(config.LoadStore())
	if _, ok := pool.Acquire("", nil); ok {
		t.Fatal("expected acquire to fail with no accounts")
	}
	status := pool.Status()
	if total, ok := status["total"].(int); !ok || total != 0 {
		t.Fatalf("unexpected total: %#v", status["total"])
	}
}

func TestPoolReleaseNonExistentAccount(t *testing.T) {
	pool := newPoolForTest(t, "2")
	pool.Release("nonexistent@example.com") // should not panic
}

func TestPoolReleaseAlreadyReleased(t *testing.T) {
	pool := newPoolForTest(t, "2")
	acc, ok := pool.Acquire("", nil)
	if !ok {
		t.Fatal("expected acquire success")
	}
	pool.Release(acc.Identifier())
	pool.Release(acc.Identifier()) // double release should not panic
}

func TestPoolAcquireTargetNotFound(t *testing.T) {
	pool := newPoolForTest(t, "2")
	if _, ok := pool.Acquire("nonexistent@example.com", nil); ok {
		t.Fatal("expected acquire to fail for non-existent target")
	}
}

func TestPoolAcquireWithExclusionList(t *testing.T) {
	pool := newPoolForTest(t, "2")
	acc, ok := pool.Acquire("", map[string]bool{"acc1@example.com": true})
	if !ok {
		t.Fatal("expected acquire success with exclusion")
	}
	if acc.Identifier() != "acc2@example.com" {
		t.Fatalf("expected acc2 when acc1 excluded, got %q", acc.Identifier())
	}
	pool.Release(acc.Identifier())
}

func TestPoolAcquireAllExcluded(t *testing.T) {
	pool := newPoolForTest(t, "2")
	if _, ok := pool.Acquire("", map[string]bool{
		"acc1@example.com": true,
		"acc2@example.com": true,
	}); ok {
		t.Fatal("expected acquire to fail when all accounts excluded")
	}
}

func TestPoolStatusFields(t *testing.T) {
	pool := newPoolForTest(t, "2")
	status := pool.Status()

	// Check all expected fields are present
	for _, key := range []string{"total", "available", "max_inflight_per_account", "recommended_concurrency", "available_accounts", "in_use_accounts", "waiting", "max_queue_size"} {
		if _, ok := status[key]; !ok {
			t.Fatalf("missing status field: %s", key)
		}
	}
}

func TestPoolStatusAccountDetails(t *testing.T) {
	pool := newPoolForTest(t, "2")
	acc, _ := pool.Acquire("acc1@example.com", nil)

	status := pool.Status()
	inUseAccounts, ok := status["in_use_accounts"].([]string)
	if !ok {
		t.Fatalf("unexpected in_use_accounts type: %T", status["in_use_accounts"])
	}
	found := false
	for _, id := range inUseAccounts {
		if id == "acc1@example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected acc1 in in_use_accounts, got %v", inUseAccounts)
	}
	if status["in_use"] != 1 {
		t.Fatalf("expected 1 in_use, got %v", status["in_use"])
	}

	pool.Release(acc.Identifier())
}

func TestPoolAcquireWaitContextCancelled(t *testing.T) {
	pool := newSingleAccountPoolForTest(t, "1")
	// Exhaust the pool
	first, ok := pool.Acquire("", nil)
	if !ok {
		t.Fatal("expected first acquire to succeed")
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	var waitOK bool
	go func() {
		defer wg.Done()
		_, waitOK = pool.AcquireWait(ctx, "", nil)
	}()

	// Wait until queued
	waitForWaitingCount(t, pool, 1)

	// Cancel context
	cancel()

	wg.Wait()
	if waitOK {
		t.Fatal("expected acquire to fail after context cancellation")
	}

	pool.Release(first.Identifier())
}

func TestPoolAcquireWaitTargetAccount(t *testing.T) {
	pool := newPoolForTest(t, "1")
	// Exhaust acc1
	acc1, ok := pool.Acquire("acc1@example.com", nil)
	if !ok {
		t.Fatal("expected acquire acc1 success")
	}

	// Acquire acc2 directly (should succeed since acc2 is free)
	ctx := context.Background()
	acc2, ok := pool.AcquireWait(ctx, "acc2@example.com", nil)
	if !ok {
		t.Fatal("expected acquire acc2 success via AcquireWait")
	}
	if acc2.Identifier() != "acc2@example.com" {
		t.Fatalf("expected acc2, got %q", acc2.Identifier())
	}

	pool.Release(acc1.Identifier())
	pool.Release(acc2.Identifier())
}

func TestPoolMaxQueueSizeOverride(t *testing.T) {
	t.Setenv("DS2API_ACCOUNT_MAX_INFLIGHT", "1")
	t.Setenv("DS2API_ACCOUNT_CONCURRENCY", "")
	t.Setenv("DS2API_ACCOUNT_MAX_QUEUE", "5")
	t.Setenv("DS2API_ACCOUNT_QUEUE_SIZE", "")
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"acc1@example.com","token":"t1"}]}`)
	pool := NewPool(config.LoadStore())
	status := pool.Status()
	if got, ok := status["max_queue_size"].(int); !ok || got != 5 {
		t.Fatalf("expected max_queue_size=5, got %#v", status["max_queue_size"])
	}
}

func TestPoolQueueSizeAliasEnv(t *testing.T) {
	t.Setenv("DS2API_ACCOUNT_MAX_INFLIGHT", "1")
	t.Setenv("DS2API_ACCOUNT_CONCURRENCY", "")
	t.Setenv("DS2API_ACCOUNT_MAX_QUEUE", "")
	t.Setenv("DS2API_ACCOUNT_QUEUE_SIZE", "7")
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"],"accounts":[{"email":"acc1@example.com","token":"t1"}]}`)
	pool := NewPool(config.LoadStore())
	status := pool.Status()
	if got, ok := status["max_queue_size"].(int); !ok || got != 7 {
		t.Fatalf("expected max_queue_size=7, got %#v", status["max_queue_size"])
	}
}

func TestPoolMultipleAcquireReleaseCycles(t *testing.T) {
	pool := newSingleAccountPoolForTest(t, "1")
	for i := 0; i < 10; i++ {
		acc, ok := pool.Acquire("", nil)
		if !ok {
			t.Fatalf("acquire failed at cycle %d", i)
		}
		pool.Release(acc.Identifier())
	}
}

func TestPoolConcurrentAcquireWait(t *testing.T) {
	pool := newSingleAccountPoolForTest(t, "1")
	first, ok := pool.Acquire("", nil)
	if !ok {
		t.Fatal("expected first acquire success")
	}

	const waiters = 3
	results := make(chan bool, waiters)

	for i := 0; i < waiters; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, ok := pool.AcquireWait(ctx, "", nil)
			results <- ok
		}()
	}

	// Wait for all to be queued (only 1 can queue)
	time.Sleep(50 * time.Millisecond)

	// Release and allow queued requests to proceed
	pool.Release(first.Identifier())

	successCount := 0
	timeoutCount := 0
	for i := 0; i < waiters; i++ {
		select {
		case ok := <-results:
			if ok {
				successCount++
				// Release for next waiter
				pool.Release("acc1@example.com")
			} else {
				timeoutCount++
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for results")
		}
	}

	// At least 1 should succeed; 2 may fail due to queue limit
	if successCount < 1 {
		t.Fatalf("expected at least 1 success, got success=%d timeout=%d", successCount, timeoutCount)
	}
}

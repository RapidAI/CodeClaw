package im

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// LLMSemaphore limits the number of concurrent LLM API calls across the
// entire Hub. It uses a buffered channel as a counting semaphore and
// supports dynamic resizing when the configuration changes.
type LLMSemaphore struct {
	mu       sync.Mutex
	sem      chan struct{}
	capacity int
	inflight int64 // atomic; for monitoring

	// Timeout for waiting to acquire a slot. Requests that cannot acquire
	// within this duration degrade gracefully.
	AcquireTimeout time.Duration
}

const defaultAcquireTimeout = 10 * time.Second

// NewLLMSemaphore creates a semaphore with the given capacity.
func NewLLMSemaphore(capacity int) *LLMSemaphore {
	if capacity <= 0 {
		capacity = DefaultMaxConcurrent
	}
	return &LLMSemaphore{
		sem:            make(chan struct{}, capacity),
		capacity:       capacity,
		AcquireTimeout: defaultAcquireTimeout,
	}
}

// Acquire blocks until a slot is available, the context is cancelled, or
// the acquire timeout elapses. Returns true if a slot was acquired.
func (s *LLMSemaphore) Acquire(ctx context.Context) bool {
	timeout := s.AcquireTimeout
	if timeout <= 0 {
		timeout = defaultAcquireTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Read sem under lock to be safe against concurrent Resize.
	s.mu.Lock()
	sem := s.sem
	s.mu.Unlock()

	select {
	case sem <- struct{}{}:
		atomic.AddInt64(&s.inflight, 1)
		return true
	case <-timer.C:
		log.Printf("[LLMSemaphore] acquire timeout (%v), inflight=%d, capacity=%d",
			timeout, atomic.LoadInt64(&s.inflight), s.capacity)
		return false
	case <-ctx.Done():
		return false
	}
}

// Release frees a slot. Must be called exactly once per successful Acquire.
func (s *LLMSemaphore) Release() {
	s.mu.Lock()
	sem := s.sem
	s.mu.Unlock()
	<-sem
	atomic.AddInt64(&s.inflight, -1)
}

// Inflight returns the current number of in-flight LLM calls.
func (s *LLMSemaphore) Inflight() int64 {
	return atomic.LoadInt64(&s.inflight)
}

// Capacity returns the current semaphore capacity.
func (s *LLMSemaphore) Capacity() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capacity
}

// Resize adjusts the semaphore capacity. This creates a new channel and
// is safe to call while Acquire/Release are in use on the old channel,
// but in-flight calls on the old channel will drain naturally.
func (s *LLMSemaphore) Resize(newCapacity int) {
	if newCapacity <= 0 {
		newCapacity = DefaultMaxConcurrent
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if newCapacity == s.capacity {
		return
	}
	log.Printf("[LLMSemaphore] resizing %d → %d", s.capacity, newCapacity)
	s.sem = make(chan struct{}, newCapacity)
	s.capacity = newCapacity
}

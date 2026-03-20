package main

import (
	"sync"
	"testing"
	"testing/quick"
)

// ---------------------------------------------------------------------------
// Property 7: 并发安全
// Multiple goroutines accessing BackgroundLoopManager concurrently must not
// cause data races.
// ---------------------------------------------------------------------------

func TestProperty7_ConcurrentSafety(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	mgr := NewBackgroundLoopManager(statusC)

	var wg sync.WaitGroup
	// Spawn, List, Get, Stop concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				kind := SlotKind(id % 3)
				ctx := mgr.Spawn(kind, "user", "task", 20, nil)
				_ = mgr.List()
				_ = mgr.ListViews()
				_ = mgr.QueueLength(kind)
				_ = mgr.RunningCount(kind)
				if ctx != nil {
					_ = mgr.Get(ctx.ID)
					mgr.Stop(ctx.ID)
				}
			}
		}(i)
	}
	wg.Wait()
	// No race detector failures = pass
}

// ---------------------------------------------------------------------------
// Property 8: Slot 并发控制
// The number of concurrently running loops per SlotKind never exceeds the limit.
// ---------------------------------------------------------------------------

func TestProperty8_SlotConcurrencyControl(t *testing.T) {
	f := func(numSpawns uint8) bool {
		n := int(numSpawns)%20 + 2 // 2..21 spawn attempts

		statusC := make(chan StatusEvent, 32)
		mgr := NewBackgroundLoopManager(statusC)

		var spawned []*LoopContext
		for i := 0; i < n; i++ {
			ctx := mgr.Spawn(SlotKindCoding, "user", "task", 20, nil)
			if ctx != nil {
				spawned = append(spawned, ctx)
			}
		}

		// Only 1 should have been spawned (limit=1 for coding)
		if mgr.RunningCount(SlotKindCoding) != 1 {
			return false
		}
		if len(spawned) != 1 {
			return false
		}

		// Stop it, then spawn again — should succeed
		mgr.Stop(spawned[0].ID)
		if mgr.RunningCount(SlotKindCoding) != 0 {
			return false
		}

		ctx2 := mgr.Spawn(SlotKindCoding, "user", "task2", 30, nil)
		if ctx2 == nil {
			return false
		}
		if mgr.RunningCount(SlotKindCoding) != 1 {
			return false
		}
		mgr.Stop(ctx2.ID)
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Slot independence: different SlotKinds don't block each other
// ---------------------------------------------------------------------------

func TestSlotIndependence(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	mgr := NewBackgroundLoopManager(statusC)

	coding := mgr.Spawn(SlotKindCoding, "u", "coding task", 20, nil)
	scheduled := mgr.Spawn(SlotKindScheduled, "u", "scheduled task", 50, nil)
	auto := mgr.Spawn(SlotKindAuto, "u", "auto task", 30, nil)

	if coding == nil || scheduled == nil || auto == nil {
		t.Fatal("all three slot kinds should spawn independently")
	}
	if mgr.RunningCount(SlotKindCoding) != 1 {
		t.Error("coding slot should have 1 running")
	}
	if mgr.RunningCount(SlotKindScheduled) != 1 {
		t.Error("scheduled slot should have 1 running")
	}
	if mgr.RunningCount(SlotKindAuto) != 1 {
		t.Error("auto slot should have 1 running")
	}

	// Trying to spawn another coding should fail
	extra := mgr.Spawn(SlotKindCoding, "u", "extra", 10, nil)
	if extra != nil {
		t.Error("should not spawn when coding slot is full")
	}

	mgr.Stop(coding.ID)
	mgr.Stop(scheduled.ID)
	mgr.Stop(auto.ID)
}

// ---------------------------------------------------------------------------
// SpawnOrQueue: queued task gets dequeued on Stop
// ---------------------------------------------------------------------------

func TestSpawnOrQueue_Dequeue(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	mgr := NewBackgroundLoopManager(statusC)

	// Fill the coding slot
	ctx1 := mgr.Spawn(SlotKindCoding, "u", "first", 20, nil)
	if ctx1 == nil {
		t.Fatal("first spawn should succeed")
	}

	// Queue a second task
	ctx2, waitCh := mgr.SpawnOrQueue(SlotKindCoding, "u", "second", 30)
	if ctx2 != nil {
		t.Fatal("should be queued, not spawned")
	}
	if waitCh == nil {
		t.Fatal("should return a wait channel")
	}
	if mgr.QueueLength(SlotKindCoding) != 1 {
		t.Fatalf("expected queue length 1, got %d", mgr.QueueLength(SlotKindCoding))
	}

	// Stop the first — should dequeue and spawn the second
	mgr.Stop(ctx1.ID)

	// Wait for the dequeued context
	dequeued := <-waitCh
	if dequeued == nil {
		t.Fatal("dequeued context should not be nil")
	}
	if dequeued.Description != "second" {
		t.Errorf("expected description 'second', got %q", dequeued.Description)
	}
	if mgr.RunningCount(SlotKindCoding) != 1 {
		t.Error("coding slot should have 1 running after dequeue")
	}
	if mgr.QueueLength(SlotKindCoding) != 0 {
		t.Error("queue should be empty after dequeue")
	}

	mgr.Stop(dequeued.ID)
}

// ---------------------------------------------------------------------------
// Complete: marks loop as completed and dequeues
// ---------------------------------------------------------------------------

func TestComplete_DequeuesNext(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	mgr := NewBackgroundLoopManager(statusC)

	ctx1 := mgr.Spawn(SlotKindScheduled, "u", "task1", 50, nil)
	if ctx1 == nil {
		t.Fatal("first spawn should succeed")
	}

	_, waitCh := mgr.SpawnOrQueue(SlotKindScheduled, "u", "task2", 50)

	mgr.Complete(ctx1.ID)

	if ctx1.State() != "completed" {
		t.Errorf("expected completed, got %s", ctx1.State())
	}

	dequeued := <-waitCh
	if dequeued == nil {
		t.Fatal("should dequeue after complete")
	}
	mgr.Stop(dequeued.ID)
}

// ---------------------------------------------------------------------------
// SendContinue
// ---------------------------------------------------------------------------

func TestSendContinue(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	mgr := NewBackgroundLoopManager(statusC)

	ctx := mgr.Spawn(SlotKindCoding, "u", "task", 20, nil)
	if ctx == nil {
		t.Fatal("spawn should succeed")
	}

	// Not paused — should error
	if err := mgr.SendContinue(ctx.ID, 10); err == nil {
		t.Error("should error when not paused")
	}

	// Set to paused
	ctx.SetState("paused")
	if err := mgr.SendContinue(ctx.ID, 20); err != nil {
		t.Errorf("should succeed when paused: %v", err)
	}

	// Read the signal
	received := <-ctx.ContinueC
	if received != 20 {
		t.Errorf("expected 20, got %d", received)
	}

	// Non-existent loop
	if err := mgr.SendContinue("nonexistent", 10); err == nil {
		t.Error("should error for nonexistent loop")
	}

	mgr.Stop(ctx.ID)
}

// ---------------------------------------------------------------------------
// ListViews snapshot correctness
// ---------------------------------------------------------------------------

func TestListViews(t *testing.T) {
	statusC := make(chan StatusEvent, 32)
	mgr := NewBackgroundLoopManager(statusC)

	ctx := mgr.Spawn(SlotKindCoding, "u", "write snake", 20, nil)
	ctx.SetIteration(5)
	ctx.SessionID = "session-abc"

	views := mgr.ListViews()
	if len(views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(views))
	}

	v := views[0]
	if v.SlotKind != "coding" {
		t.Errorf("expected coding, got %s", v.SlotKind)
	}
	if v.Description != "write snake" {
		t.Errorf("expected 'write snake', got %s", v.Description)
	}
	if v.Iteration != 5 {
		t.Errorf("expected iteration 5, got %d", v.Iteration)
	}
	if v.MaxIter != 20 {
		t.Errorf("expected max 20, got %d", v.MaxIter)
	}
	if v.SessionID != "session-abc" {
		t.Errorf("expected session-abc, got %s", v.SessionID)
	}
	if v.Status != "running" {
		t.Errorf("expected running, got %s", v.Status)
	}

	mgr.Stop(ctx.ID)
}

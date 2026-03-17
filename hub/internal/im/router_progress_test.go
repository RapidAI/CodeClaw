package im

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestRouteToAgent_ProgressResetsTimeout verifies that progress updates
// from the Agent reset the response timeout, preventing 504 on long tasks.
func TestRouteToAgent_ProgressResetsTimeout(t *testing.T) {
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	var mu sync.Mutex
	var progressTexts []string

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		mu.Lock()
		progressTexts = append(progressTexts, text)
		mu.Unlock()
	})

	// Start RouteToAgent in a goroutine.
	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "查找视频文件")
		resultCh <- resp
	}()

	// Wait for the pending request to be created.
	time.Sleep(50 * time.Millisecond)

	router.mu.Lock()
	var reqID string
	var pending *PendingIMRequest
	for id, p := range router.pendingReqs {
		reqID = id
		pending = p
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	// Override timeout to 200ms for testing.
	pending.Timeout = 200 * time.Millisecond

	// Send a progress update after 100ms (before timeout).
	time.Sleep(100 * time.Millisecond)
	router.HandleAgentProgress(reqID, "⚙️ 正在执行工具: bash")

	// Wait another 100ms — without progress, this would have timed out.
	// But progress reset the timer, so we have another 200ms.
	time.Sleep(100 * time.Millisecond)

	// Now send the final response.
	router.HandleAgentResponse(reqID, &AgentResponse{
		Text: "找到 5 个视频文件。",
	})

	// Wait for result.
	select {
	case resp := <-resultCh:
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, resp.Body)
		}
		if resp.Body != "找到 5 个视频文件。" {
			t.Fatalf("unexpected body: %s", resp.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}

	// Verify progress was delivered (throttle interval is 10s, first one
	// should always be delivered immediately).
	mu.Lock()
	defer mu.Unlock()
	if len(progressTexts) != 1 {
		t.Fatalf("expected 1 progress text, got %d", len(progressTexts))
	}
	if progressTexts[0] != "⚙️ 正在执行工具: bash" {
		t.Fatalf("unexpected progress text: %s", progressTexts[0])
	}
}

// TestRouteToAgent_TimeoutWithProgressInfo verifies that when a timeout
// occurs after progress was received, the error message includes the last
// progress status.
func TestRouteToAgent_TimeoutWithProgressInfo(t *testing.T) {
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {})

	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "搜索文件")
		resultCh <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	router.mu.Lock()
	var reqID string
	var pending *PendingIMRequest
	for id, p := range router.pendingReqs {
		reqID = id
		pending = p
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	// Set very short timeout.
	pending.Timeout = 150 * time.Millisecond

	// Send progress.
	time.Sleep(50 * time.Millisecond)
	router.HandleAgentProgress(reqID, "⏳ 命令仍在执行中（已 30s）: find / -name *.mp4")

	// Let it timeout after the progress reset.
	select {
	case resp := <-resultCh:
		if resp.StatusCode != 504 {
			t.Fatalf("expected 504, got %d", resp.StatusCode)
		}
		if !containsStr(resp.Body, "命令仍在执行中") {
			t.Fatalf("expected progress info in timeout body, got: %s", resp.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timeout response")
	}
}

// TestRouteToAgent_ProgressThrottling verifies that rapid progress updates
// are throttled so the user doesn't get flooded with messages.
func TestRouteToAgent_ProgressThrottling(t *testing.T) {
	df := &mockDeviceFinder{machineID: "m1", llmConfigured: true, found: true}
	router := NewMessageRouter(df)
	defer router.Stop()

	var mu sync.Mutex
	var deliveredCount int

	router.SetProgressDelivery(func(ctx context.Context, userID, platformName, platformUID, text string) {
		mu.Lock()
		deliveredCount++
		mu.Unlock()
	})

	resultCh := make(chan *GenericResponse, 1)
	go func() {
		resp, _ := router.RouteToAgent(context.Background(), "user1", "test", "uid1", "搜索文件")
		resultCh <- resp
	}()

	time.Sleep(50 * time.Millisecond)

	router.mu.Lock()
	var reqID string
	for id := range router.pendingReqs {
		reqID = id
		break
	}
	router.mu.Unlock()

	if reqID == "" {
		t.Fatal("expected a pending request")
	}

	// Send 5 rapid progress updates (within the 10s throttle window).
	for i := 0; i < 5; i++ {
		router.HandleAgentProgress(reqID, "progress")
		time.Sleep(10 * time.Millisecond)
	}

	// Send final response.
	router.HandleAgentResponse(reqID, &AgentResponse{Text: "done"})

	select {
	case resp := <-resultCh:
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	// Only the first progress should have been delivered (others throttled).
	mu.Lock()
	defer mu.Unlock()
	if deliveredCount != 1 {
		t.Fatalf("expected 1 delivered progress (throttled), got %d", deliveredCount)
	}
}

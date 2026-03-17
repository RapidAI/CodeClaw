package im

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Dependency interfaces for MessageRouter
// ---------------------------------------------------------------------------

// DeviceFinder abstracts the device service for looking up user machines.
type DeviceFinder interface {
	// FindOnlineMachineForUser returns the machine ID of an online device
	// belonging to the given user. Returns ("", false) if no device is online.
	FindOnlineMachineForUser(ctx context.Context, userID string) (machineID string, llmConfigured bool, found bool)
	// SendToMachine sends a JSON-serialisable message to the machine via WebSocket.
	SendToMachine(machineID string, msg any) error
}

// ---------------------------------------------------------------------------
// PendingIMRequest — tracks an in-flight IM → Agent request
// ---------------------------------------------------------------------------

// PendingIMRequest represents a message waiting for the Agent's reply.
type PendingIMRequest struct {
	RequestID   string
	UserID      string
	PlatformUID string // original platform-specific user ID for progress delivery
	Text        string
	ResponseCh  chan *AgentResponse
	CreatedAt   time.Time
	Timeout     time.Duration

	// ProgressCh receives progress text updates from the Agent. Each update
	// resets the response timeout so long-running tasks don't expire.
	ProgressCh chan string

	// LastActivity tracks the most recent progress or creation time.
	// Used by cleanupExpired to avoid premature reaping of requests
	// that are being kept alive by progress updates.
	lastActivity time.Time
}

// defaultAgentTimeout is the maximum time to wait for an Agent response.
// 多轮 Agent 循环（最多 12 轮 LLM 调用）可能需要较长时间
const defaultAgentTimeout = 180 * time.Second

// cleanupInterval controls how often expired pending requests are reaped.
const cleanupInterval = 30 * time.Second

// ---------------------------------------------------------------------------
// MessageRouter — routes IM messages to MaClaw Agent via WebSocket
// ---------------------------------------------------------------------------

// ProgressDeliveryFunc is called to deliver progress text to a user via IM.
type ProgressDeliveryFunc func(ctx context.Context, userID, platformName, platformUID, text string)

// MessageRouter replaces the old NL_Router + BridgeExecutor pipeline.
// It transparently relays IM messages to the user's MaClaw client Agent
// and waits for the Agent's response.
type MessageRouter struct {
	devices          DeviceFinder
	progressDelivery ProgressDeliveryFunc

	mu          sync.Mutex
	pendingReqs map[string]*PendingIMRequest // requestID → pending

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewMessageRouter creates a MessageRouter with the given device finder.
func NewMessageRouter(devices DeviceFinder) *MessageRouter {
	r := &MessageRouter{
		devices:     devices,
		pendingReqs: make(map[string]*PendingIMRequest),
		stopCh:      make(chan struct{}),
	}
	go r.cleanupLoop()
	return r
}

// SetProgressDelivery configures the function used to deliver progress
// updates to users via IM. Called by the Adapter after construction.
func (r *MessageRouter) SetProgressDelivery(fn ProgressDeliveryFunc) {
	r.progressDelivery = fn
}

// Stop terminates the background cleanup goroutine.
func (r *MessageRouter) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}

// RouteToAgent sends the user's IM message to their bound MaClaw device
// and blocks until the Agent replies or the timeout expires.
//
// Preconditions: identity mapping and rate limiting have already been applied
// by the Adapter before calling this method.
func (r *MessageRouter) RouteToAgent(ctx context.Context, userID, platformName, platformUID, text string) (*GenericResponse, error) {
	// 1. Find the user's online device.
	machineID, llmConfigured, found := r.devices.FindOnlineMachineForUser(ctx, userID)
	if !found {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "📴",
			Title:      "设备不在线",
			Body:       "您的设备当前不在线，无法处理请求。\n\n请确认 MaClaw 客户端已启动并连接到 Hub。",
		}, nil
	}

	// 2. Check LLM configuration.
	if !llmConfigured {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "⚠️",
			Title:      "Agent 未就绪",
			Body:       "设备已在线，但 MaClaw LLM 未配置。Agent 无法运行。\n\n请在 MaClaw 客户端的设置中配置 LLM（URL、Key、Model），然后重试。",
		}, nil
	}

	// 3. Create pending request.
	requestID := fmt.Sprintf("im_%s_%d", userID, time.Now().UnixNano())
	now := time.Now()
	pending := &PendingIMRequest{
		RequestID:    requestID,
		UserID:       userID,
		PlatformUID:  platformUID,
		Text:         text,
		ResponseCh:   make(chan *AgentResponse, 1),
		ProgressCh:   make(chan string, 8),
		CreatedAt:    now,
		Timeout:      defaultAgentTimeout,
		lastActivity: now,
	}

	r.mu.Lock()
	r.pendingReqs[requestID] = pending
	r.mu.Unlock()

	// Ensure cleanup on all exit paths.
	defer func() {
		r.mu.Lock()
		delete(r.pendingReqs, requestID)
		r.mu.Unlock()
	}()

	// 4. Send im.user_message to MaClaw client via WebSocket.
	wsMsg := map[string]interface{}{
		"type":       "im.user_message",
		"request_id": requestID,
		"ts":         time.Now().Unix(),
		"payload": map[string]interface{}{
			"user_id":  userID,
			"platform": platformName,
			"text":     text,
		},
	}
	if err := r.devices.SendToMachine(machineID, wsMsg); err != nil {
		log.Printf("[MessageRouter] SendToMachine failed for machine=%s: %v", machineID, err)
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "📴",
			Title:      "发送失败",
			Body:       "无法将消息发送到您的设备，请检查连接状态。",
		}, nil
	}

	// 5. Wait for Agent response with resettable timeout.
	// Progress updates from the Agent reset the timer, preventing 504 on
	// long-running tasks like file search or large builds.
	timer := time.NewTimer(pending.Timeout)
	defer timer.Stop()

	// progressTexts collects progress messages; lastDelivered throttles IM sends.
	var progressTexts []string
	var lastDelivered time.Time
	const progressMinInterval = 10 * time.Second

	for {
		select {
		case resp := <-pending.ResponseCh:
			if resp == nil {
				return &GenericResponse{
					StatusCode: 500,
					StatusIcon: "❌",
					Title:      "Agent 返回空响应",
					Body:       "Agent 未返回有效回复，请稍后重试。",
				}, nil
			}
			return resp.ToGenericResponse(), nil

		case progressText := <-pending.ProgressCh:
			// Reset the timeout — the Agent is still alive and working.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(pending.Timeout)
			progressTexts = append(progressTexts, progressText)

			// Throttle IM delivery: at most once per progressMinInterval
			// to avoid flooding the user with status messages.
			if time.Since(lastDelivered) >= progressMinInterval {
				lastDelivered = time.Now()
				go r.deliverProgress(ctx, userID, platformName, platformUID, progressText)
			}

		case <-timer.C:
			body := "Agent 在 180 秒内未回复，请稍后重试。\n\n可能原因：LLM 服务响应缓慢或不可用。"
			if len(progressTexts) > 0 {
				body = fmt.Sprintf("Agent 任务执行超时。最后状态：%s\n\n任务可能仍在后台运行，请稍后查询结果。", progressTexts[len(progressTexts)-1])
			}
			return &GenericResponse{
				StatusCode: 504,
				StatusIcon: "⏰",
				Title:      "Agent 响应超时",
				Body:       body,
			}, nil

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// HandleAgentResponse is called when the Hub receives an "im.agent_response"
// message from a MaClaw client. It matches the response to the pending
// request by requestID and delivers it.
func (r *MessageRouter) HandleAgentResponse(requestID string, resp *AgentResponse) {
	r.mu.Lock()
	pending, ok := r.pendingReqs[requestID]
	r.mu.Unlock()

	if !ok {
		log.Printf("[MessageRouter] received agent response for unknown request_id=%s (expired or already handled)", requestID)
		return
	}

	// Non-blocking send — the channel is buffered with size 1.
	select {
	case pending.ResponseCh <- resp:
	default:
		log.Printf("[MessageRouter] response channel full for request_id=%s, dropping", requestID)
	}
}

// HandleAgentProgress is called when the Hub receives an "im.agent_progress"
// message from a MaClaw client. It delivers the progress text to the pending
// request's ProgressCh, which resets the response timeout in RouteToAgent.
func (r *MessageRouter) HandleAgentProgress(requestID string, text string) {
	r.mu.Lock()
	pending, ok := r.pendingReqs[requestID]
	if ok {
		pending.lastActivity = time.Now()
	}
	r.mu.Unlock()

	if !ok {
		return
	}

	// Non-blocking send — drop if the channel is full (shouldn't happen
	// with buffer size 8, but be safe).
	select {
	case pending.ProgressCh <- text:
	default:
		log.Printf("[MessageRouter] progress channel full for request_id=%s, dropping", requestID)
	}
}

// deliverProgress sends a progress text message to the user via IM.
// This is a best-effort delivery — errors are logged but not propagated.
func (r *MessageRouter) deliverProgress(ctx context.Context, userID, platformName, platformUID, text string) {
	if r.progressDelivery != nil {
		r.progressDelivery(ctx, userID, platformName, platformUID, text)
	}
}

// cleanupLoop periodically removes expired pending requests.
func (r *MessageRouter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cleanupExpired()
		case <-r.stopCh:
			return
		}
	}
}

// cleanupExpired removes pending requests that have exceeded their timeout
// without any recent activity (creation or progress update).
func (r *MessageRouter) cleanupExpired() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, req := range r.pendingReqs {
		if now.Sub(req.lastActivity) > req.Timeout+10*time.Second {
			delete(r.pendingReqs, id)
		}
	}
}

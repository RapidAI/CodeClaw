package guiautomation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// guiTaskEntry wraps GUITaskState with control channels.
type guiTaskEntry struct {
	state   *GUITaskState
	cancel  context.CancelFunc
	pauseC  chan struct{}
	resumeC chan struct{}
}

// GUITaskSupervisor manages GUI task execution with retry, pause/resume/cancel.
type GUITaskSupervisor struct {
	mu           sync.RWMutex
	tasks        map[string]*guiTaskEntry
	locator      *ElementLocator
	input        InputSimulator
	screenshotFn func() (string, error)
	ocr          browser.OCRProvider
	retrier      *GUIRetryStrategy
	logger       func(string)
	idCounter    int
}

// NewGUITaskSupervisor creates a supervisor.
func NewGUITaskSupervisor(
	locator *ElementLocator,
	input InputSimulator,
	screenshotFn func() (string, error),
	ocr browser.OCRProvider,
	retrier *GUIRetryStrategy,
	logger func(string),
) *GUITaskSupervisor {
	if retrier == nil {
		retrier = NewGUIRetryStrategy(3)
	}
	return &GUITaskSupervisor{
		tasks:        make(map[string]*guiTaskEntry),
		locator:      locator,
		input:        input,
		screenshotFn: screenshotFn,
		ocr:          ocr,
		retrier:      retrier,
		logger:       logger,
	}
}

// Execute runs a GUI task. It blocks until the task completes, fails, or is cancelled.
func (s *GUITaskSupervisor) Execute(spec GUITaskSpec) (*GUITaskState, error) {
	if spec.MaxRetries <= 0 {
		spec.MaxRetries = 3
	}
	if spec.StepTimeout <= 0 {
		spec.StepTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.mu.Lock()
	s.idCounter++
	if spec.ID == "" {
		spec.ID = fmt.Sprintf("gui-%d", s.idCounter)
	}
	state := &GUITaskState{
		ID:         spec.ID,
		Status:     "running",
		TotalSteps: len(spec.Steps),
		StartedAt:  time.Now(),
	}
	entry := &guiTaskEntry{
		state:   state,
		cancel:  cancel,
		pauseC:  make(chan struct{}, 1),
		resumeC: make(chan struct{}, 1),
	}
	s.tasks[spec.ID] = entry
	s.mu.Unlock()

	s.log("gui task %s started: %s (%d steps)", spec.ID, spec.Description, len(spec.Steps))

	for i, step := range spec.Steps {
		if err := ctx.Err(); err != nil {
			state.Status = "cancelled"
			state.LastError = "cancelled by user"
			return state, fmt.Errorf("cancelled")
		}

		state.CurrentStep = i + 1

		err := s.executeStepWithRetry(ctx, spec, step, i, state)
		if err != nil {
			state.Status = "failed"
			state.LastError = err.Error()
			s.log("gui task %s failed at step %d: %v", spec.ID, i+1, err)
			return state, err
		}

		// Record checkpoint after each step
		s.takeCheckpoint(state, i, step)

		// Check for pause signal
		s.mu.RLock()
		e := s.tasks[spec.ID]
		s.mu.RUnlock()
		if e != nil {
			select {
			case <-e.pauseC:
				state.Status = "paused"
				s.log("gui task %s paused after step %d", spec.ID, i+1)
				select {
				case <-e.resumeC:
					state.Status = "running"
					s.log("gui task %s resumed", spec.ID)
				case <-ctx.Done():
					state.Status = "cancelled"
					state.LastError = "cancelled while paused"
					return state, fmt.Errorf("cancelled while paused")
				}
			default:
			}
		}
	}

	state.Status = "completed"
	s.log("gui task %s completed successfully", spec.ID)
	return state, nil
}

// Pause requests a running task to pause after the current step.
func (s *GUITaskSupervisor) Pause(taskID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if entry.state.Status != "running" {
		return fmt.Errorf("task %s is not running (status=%s)", taskID, entry.state.Status)
	}
	select {
	case entry.pauseC <- struct{}{}:
	default:
	}
	return nil
}

// Resume resumes a paused task.
func (s *GUITaskSupervisor) Resume(taskID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if entry.state.Status != "paused" {
		return fmt.Errorf("task %s is not paused (status=%s)", taskID, entry.state.Status)
	}
	select {
	case entry.resumeC <- struct{}{}:
	default:
	}
	return nil
}

// Cancel cancels a running or paused task.
func (s *GUITaskSupervisor) Cancel(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	st := entry.state.Status
	if st != "running" && st != "paused" {
		return fmt.Errorf("task %s is not running or paused (status=%s)", taskID, st)
	}
	entry.cancel()
	return nil
}

// GetState returns the current state of a task.
func (s *GUITaskSupervisor) GetState(taskID string) (*GUITaskState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tasks[taskID]
	if !ok {
		return nil, false
	}
	return entry.state, true
}

// ── internal ──

func (s *GUITaskSupervisor) executeStepWithRetry(ctx context.Context, spec GUITaskSpec, step GUIStepSpec, stepIdx int, state *GUITaskState) error {
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = spec.StepTimeout
	}

	currentTimeout := timeout
	for retry := 0; ; retry++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cancelled")
		}

		err := s.executeOneStep(ctx, step, currentTimeout)
		if err == nil {
			return nil
		}

		failType := s.retrier.ClassifyFailure(err)

		// Capture screenshot and OCR for retry decision
		screenshotB64 := ""
		ocrText := ""
		if s.screenshotFn != nil {
			if img, serr := s.screenshotFn(); serr == nil {
				screenshotB64 = img
			}
		}
		if s.ocr != nil && s.ocr.IsAvailable() && screenshotB64 != "" {
			if results, oerr := s.ocr.Recognize(screenshotB64); oerr == nil {
				for _, r := range results {
					ocrText += r.Text + " "
				}
			}
		}

		decision := s.retrier.Decide(failType, step, retry, screenshotB64, ocrText)
		if !decision.ShouldRetry {
			return fmt.Errorf("step %d failed: %v (%s)", stepIdx+1, err, decision.Reason)
		}

		s.log("gui task step %d retry %d: %s", stepIdx+1, retry+1, decision.Reason)
		state.RetryCount++

		if decision.WaitBefore > 0 {
			time.Sleep(decision.WaitBefore)
		}
		if decision.AdjustedTimeout > 0 {
			currentTimeout = decision.AdjustedTimeout
		}
	}
}

func (s *GUITaskSupervisor) executeOneStep(ctx context.Context, step GUIStepSpec, timeout time.Duration) error {
	stepCtx, stepCancel := context.WithTimeout(ctx, timeout)
	defer stepCancel()

	ch := make(chan error, 1)
	go func() {
		ch <- s.doStep(step)
	}()

	select {
	case err := <-ch:
		return err
	case <-stepCtx.Done():
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("step timed out after %v", timeout)
	}
}

func (s *GUITaskSupervisor) doStep(step GUIStepSpec) error {
	// If we have the original recorded step, use the locator to find the element
	var x, y int
	if step.OrigStep != nil && s.locator != nil {
		result, err := s.locator.Locate(*step.OrigStep)
		if err != nil {
			return fmt.Errorf("element not found: %w", err)
		}
		x, y = result.X, result.Y
	} else {
		// Fall back to params
		fmt.Sscanf(step.Params["x"], "%d", &x)
		fmt.Sscanf(step.Params["y"], "%d", &y)
	}

	switch step.Action {
	case "click":
		return s.input.Click(x, y)
	case "right_click":
		return s.input.RightClick(x, y)
	case "double_click":
		return s.input.DoubleClick(x, y)
	case "type":
		text := step.Params["text"]
		// Click the target first, then type
		if x > 0 || y > 0 {
			if err := s.input.Click(x, y); err != nil {
				return err
			}
		}
		return s.input.Type(text)
	case "keypress":
		keys := step.Params["keys"]
		if keys == "" {
			return fmt.Errorf("keypress: missing keys param")
		}
		// keys is comma-separated
		var keyList []string
		for _, k := range splitKeys(keys) {
			keyList = append(keyList, k)
		}
		return s.input.KeyCombo(keyList...)
	case "scroll":
		dy := 0
		fmt.Sscanf(step.Params["scroll_dy"], "%d", &dy)
		return s.input.Scroll(x, y, 0, dy)
	case "drag":
		toX, toY := 0, 0
		fmt.Sscanf(step.Params["drag_to_x"], "%d", &toX)
		fmt.Sscanf(step.Params["drag_to_y"], "%d", &toY)
		return s.input.DragDrop(x, y, toX, toY)
	default:
		return fmt.Errorf("unknown GUI action: %s", step.Action)
	}
}

func (s *GUITaskSupervisor) takeCheckpoint(state *GUITaskState, stepIdx int, step GUIStepSpec) {
	cp := GUICheckpoint{
		StepIndex: stepIdx,
		Timestamp: time.Now(),
		Strategy:  "coordinate", // default; overridden below if OrigStep present
	}

	if step.OrigStep != nil {
		cp.WindowTitle = step.OrigStep.WindowTitle
		// Infer strategy from what locator info was available (without re-running Locate)
		if step.OrigStep.AccessibilityID != nil {
			cp.Strategy = string(StrategyAccessibility)
		} else if step.OrigStep.SnapshotRef != "" {
			cp.Strategy = string(StrategyImage)
		}
	}

	// Try to capture screenshot
	if s.screenshotFn != nil {
		if img, err := s.screenshotFn(); err == nil {
			cp.ScreenshotB64 = img
		}
	}

	const maxCheckpoints = 20
	state.Checkpoints = append(state.Checkpoints, cp)
	if len(state.Checkpoints) > maxCheckpoints {
		state.Checkpoints = state.Checkpoints[len(state.Checkpoints)-maxCheckpoints:]
	}
	// Only keep screenshot on the most recent checkpoint
	for i := 0; i < len(state.Checkpoints)-1; i++ {
		state.Checkpoints[i].ScreenshotB64 = ""
	}
}

func (s *GUITaskSupervisor) log(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger(fmt.Sprintf(format, args...))
	}
}

// splitKeys splits a comma-separated key string.
func splitKeys(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ScheduledTask represents a single scheduled task.
type ScheduledTask struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Action      string     `json:"action"`       // what the agent should do (natural language)
	Hour        int        `json:"hour"`          // 0-23
	Minute      int        `json:"minute"`        // 0-59
	DayOfWeek   int        `json:"day_of_week"`   // -1=every day, 0=Sun..6=Sat
	DayOfMonth  int        `json:"day_of_month"`  // -1=any, 1-31
	StartDate   string     `json:"start_date,omitempty"` // "2006-01-02", empty=no limit
	EndDate     string     `json:"end_date,omitempty"`   // "2006-01-02", empty=no limit
	Status      string     `json:"status"`        // "active", "paused", "expired"
	CreatedAt   time.Time  `json:"created_at"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	NextRunAt   *time.Time `json:"next_run_at,omitempty"`
	RunCount    int        `json:"run_count"`
	LastResult  string     `json:"last_result,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	TaskType    string     `json:"task_type,omitempty"`
}

// TaskExecutor is called when a task fires. It receives the task action
// (natural language) and should send it to the agent for processing.
type TaskExecutor func(task *ScheduledTask) (result string, err error)

// ScheduledTaskManager manages scheduled tasks with JSON persistence
// and a background ticker that fires due tasks.
type ScheduledTaskManager struct {
	mu        sync.RWMutex
	tasks     []ScheduledTask
	path      string
	stopCh    chan struct{}
	running   bool
	executor  TaskExecutor
	onChange  func() // optional callback after task state changes (fire/expire)
}

// NewScheduledTaskManager creates a manager persisting to the given path.
func NewScheduledTaskManager(path string) (*ScheduledTaskManager, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("scheduled_task: resolve path: %w", err)
	}
	m := &ScheduledTaskManager{
		tasks:  make([]ScheduledTask, 0),
		path:   absPath,
		stopCh: make(chan struct{}),
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	// Remove expired tasks and recalculate next run times.
	now := time.Now()
	var active []ScheduledTask
	for i := range m.tasks {
		if m.tasks[i].Status == "expired" || m.isExpired(&m.tasks[i], now) {
			continue // drop expired tasks
		}
		if m.tasks[i].Status == "active" {
			next := m.calcNext(&m.tasks[i], now)
			m.tasks[i].NextRunAt = next
		}
		active = append(active, m.tasks[i])
	}
	m.tasks = active
	_ = m.save()
	return m, nil
}

// SetExecutor sets the callback invoked when a task fires.
func (m *ScheduledTaskManager) SetExecutor(fn TaskExecutor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executor = fn
}

// SetOnChange sets an optional callback invoked after task state changes
// (e.g. after a task fires or expires), useful for notifying the frontend.
func (m *ScheduledTaskManager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// Start begins the background scheduler (checks every 30s).
func (m *ScheduledTaskManager) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()
	go m.loop()
	fmt.Println("[ScheduledTaskManager] started")
}

// Stop halts the scheduler.
func (m *ScheduledTaskManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		close(m.stopCh)
		m.running = false
		fmt.Println("[ScheduledTaskManager] stopped")
	}
}

func (m *ScheduledTaskManager) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

func (m *ScheduledTaskManager) tick() {
	now := time.Now()
	m.mu.RLock()
	var dueIDs []string
	for _, t := range m.tasks {
		if t.Status == "active" && t.NextRunAt != nil && !now.Before(*t.NextRunAt) {
			dueIDs = append(dueIDs, t.ID)
		}
	}
	executor := m.executor
	m.mu.RUnlock()

	for _, id := range dueIDs {
		go m.fireByID(id, executor)
	}

	// Auto-delete expired tasks so they don't clutter the list.
	m.purgeExpired(now)
}

// purgeExpired removes tasks whose status is "expired" or whose EndDate has
// passed while they were active/paused. Returns true if any tasks were removed.
func (m *ScheduledTaskManager) purgeExpired(now time.Time) bool {
	m.mu.Lock()
	n := 0
	for i := range m.tasks {
		expired := m.tasks[i].Status == "expired"
		if !expired && m.isExpired(&m.tasks[i], now) {
			expired = true
		}
		if !expired {
			m.tasks[n] = m.tasks[i]
			n++
		}
	}
	removed := len(m.tasks) - n
	if removed == 0 {
		m.mu.Unlock()
		return false
	}
	m.tasks = m.tasks[:n]
	_ = m.save()
	cb := m.onChange
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
	fmt.Printf("[ScheduledTaskManager] purged %d expired task(s)\n", removed)
	return true
}

func (m *ScheduledTaskManager) fireByID(id string, executor TaskExecutor) {
	// Atomically claim the task: read it and clear NextRunAt so the next
	// tick() won't fire it again while the executor is still running.
	m.mu.Lock()
	var taskCopy *ScheduledTask
	for i, t := range m.tasks {
		if t.ID == id {
			if t.NextRunAt == nil {
				// Already claimed by another goroutine.
				m.mu.Unlock()
				return
			}
			cp := t // copy the struct
			taskCopy = &cp
			m.tasks[i].NextRunAt = nil // prevent double-fire
			break
		}
	}
	m.mu.Unlock()
	if taskCopy == nil {
		return
	}

	fmt.Printf("[ScheduledTaskManager] firing task %s (%s)\n", taskCopy.ID, taskCopy.Name)

	// Execute outside lock (with panic recovery).
	var result, errStr string
	if executor != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					errStr = fmt.Sprintf("panic: %v", r)
				}
			}()
			res, err := executor(taskCopy)
			result = res
			if err != nil {
				errStr = err.Error()
			}
		}()
	} else {
		result = "no executor configured"
		fmt.Printf("[ScheduledTaskManager] WARNING: no executor for task %s\n", id)
	}

	// Update state under lock.
	now := time.Now()
	m.mu.Lock()
	for i := range m.tasks {
		if m.tasks[i].ID != id {
			continue
		}
		m.tasks[i].LastRunAt = &now
		m.tasks[i].RunCount++
		m.tasks[i].LastResult = truncateStr(result, 500)
		m.tasks[i].LastError = errStr

		if m.isExpired(&m.tasks[i], now) {
			// Remove the expired task in-place instead of keeping it around.
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			fmt.Printf("[ScheduledTaskManager] auto-deleted expired task %s\n", id)
		} else {
			m.tasks[i].NextRunAt = m.calcNext(&m.tasks[i], now)
		}
		break
	}
	_ = m.save()
	cb := m.onChange
	m.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

// Add creates a new scheduled task and returns its ID.
func (m *ScheduledTaskManager) Add(t ScheduledTask) (string, error) {
	if t.Name == "" {
		return "", fmt.Errorf("scheduled_task: name is required")
	}
	if t.Action == "" {
		return "", fmt.Errorf("scheduled_task: action is required")
	}
	if t.Hour < 0 || t.Hour > 23 {
		return "", fmt.Errorf("scheduled_task: hour must be 0-23")
	}
	if t.Minute < 0 || t.Minute > 59 {
		return "", fmt.Errorf("scheduled_task: minute must be 0-59")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	t.ID = generateID()
	t.Status = "active"
	t.CreatedAt = now
	t.NextRunAt = m.calcNext(&t, now)

	if m.isExpired(&t, now) {
		return "", fmt.Errorf("scheduled_task: end_date is already in the past")
	}

	m.tasks = append(m.tasks, t)
	if err := m.save(); err != nil {
		return "", err
	}
	return t.ID, nil
}

// List returns all tasks.
func (m *ScheduledTaskManager) List() []ScheduledTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ScheduledTask, len(m.tasks))
	copy(out, m.tasks)
	return out
}

// Get returns a task by ID, or nil if not found.
func (m *ScheduledTaskManager) Get(id string) *ScheduledTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tasks {
		if t.ID == id {
			cp := t
			return &cp
		}
	}
	return nil
}

// Delete removes a task by ID.
func (m *ScheduledTaskManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tasks {
		if t.ID == id {
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return m.save()
		}
	}
	return fmt.Errorf("scheduled_task: task %q not found", id)
}

// DeleteByName removes a task by name (first match).
func (m *ScheduledTaskManager) DeleteByName(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tasks {
		if t.Name == name {
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return m.save()
		}
	}
	return fmt.Errorf("scheduled_task: task named %q not found", name)
}

// Pause pauses a task.
func (m *ScheduledTaskManager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			m.tasks[i].Status = "paused"
			m.tasks[i].NextRunAt = nil
			return m.save()
		}
	}
	return fmt.Errorf("scheduled_task: task %q not found", id)
}

// Resume resumes a paused task.
func (m *ScheduledTaskManager) Resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			if m.isExpired(&m.tasks[i], now) {
				// Task has expired while paused — remove it.
				m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
				_ = m.save()
				return fmt.Errorf("scheduled_task: task %q has expired (end_date passed) and was removed", id)
			}
			m.tasks[i].Status = "active"
			m.tasks[i].NextRunAt = m.calcNext(&m.tasks[i], now)
			return m.save()
		}
	}
	return fmt.Errorf("scheduled_task: task %q not found", id)
}

// ClearAll removes all tasks.
func (m *ScheduledTaskManager) ClearAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = m.tasks[:0]
	return m.save()
}

// Update modifies a task by ID. Only non-zero/non-empty fields in args are applied.
func (m *ScheduledTaskManager) Update(id string, args map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID != id {
			continue
		}
		if v, ok := args["name"].(string); ok && v != "" {
			m.tasks[i].Name = v
		}
		if v, ok := args["action"].(string); ok && v != "" {
			m.tasks[i].Action = v
		}
		if v, ok := args["hour"].(float64); ok {
			h := int(v)
			if h >= 0 && h <= 23 {
				m.tasks[i].Hour = h
			}
		}
		if v, ok := args["minute"].(float64); ok {
			mn := int(v)
			if mn >= 0 && mn <= 59 {
				m.tasks[i].Minute = mn
			}
		}
		if v, ok := args["day_of_week"].(float64); ok {
			m.tasks[i].DayOfWeek = int(v)
		}
		if v, ok := args["day_of_month"].(float64); ok {
			m.tasks[i].DayOfMonth = int(v)
		}
		if v, ok := args["start_date"].(string); ok {
			m.tasks[i].StartDate = v
		}
		if v, ok := args["end_date"].(string); ok {
			m.tasks[i].EndDate = v
		}
		now := time.Now()
		if m.tasks[i].Status == "active" {
			m.tasks[i].NextRunAt = m.calcNext(&m.tasks[i], now)
		}
		return m.save()
	}
	return fmt.Errorf("scheduled_task: task %q not found", id)
}

// ---------------------------------------------------------------------------
// Schedule calculation
// ---------------------------------------------------------------------------

// TriggerNow immediately executes a task regardless of its schedule.
// The task must be in "active" status. Execution happens asynchronously
// in a goroutine (same as scheduled fires); the method returns immediately.
func (m *ScheduledTaskManager) TriggerNow(id string) error {
	m.mu.RLock()
	var found bool
	var status string
	for _, t := range m.tasks {
		if t.ID == id {
			found = true
			status = t.Status
			break
		}
	}
	executor := m.executor
	m.mu.RUnlock()

	if !found {
		return fmt.Errorf("task %s not found", id)
	}
	if status != "active" {
		return fmt.Errorf("task %s is not active (status=%s)", id, status)
	}

	// Fire asynchronously so the UI gets an immediate response.
	go m.fireManual(id, executor)
	return nil
}

// fireManual executes a task triggered manually. Unlike fireByID it does not
// check NextRunAt (the task may not be due yet).
func (m *ScheduledTaskManager) fireManual(id string, executor TaskExecutor) {
	m.mu.RLock()
	var taskCopy *ScheduledTask
	for _, t := range m.tasks {
		if t.ID == id {
			cp := t
			taskCopy = &cp
			break
		}
	}
	m.mu.RUnlock()
	if taskCopy == nil {
		return
	}

	fmt.Printf("[ScheduledTaskManager] manual trigger task %s (%s)\n", taskCopy.ID, taskCopy.Name)

	var result, errStr string
	if executor != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					errStr = fmt.Sprintf("panic: %v", r)
				}
			}()
			res, err := executor(taskCopy)
			result = res
			if err != nil {
				errStr = err.Error()
			}
		}()
	} else {
		result = "no executor configured"
	}

	now := time.Now()
	m.mu.Lock()
	for i := range m.tasks {
		if m.tasks[i].ID != id {
			continue
		}
		m.tasks[i].LastRunAt = &now
		m.tasks[i].RunCount++
		m.tasks[i].LastResult = truncateStr(result, 500)
		m.tasks[i].LastError = errStr
		break
	}
	_ = m.save()
	cb := m.onChange
	m.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// calcNext computes the next execution time after `after`.
func (m *ScheduledTaskManager) calcNext(t *ScheduledTask, after time.Time) *time.Time {
	// Start from the target time today.
	candidate := time.Date(after.Year(), after.Month(), after.Day(), t.Hour, t.Minute, 0, 0, time.Local)

	// If candidate is not after `after`, move to tomorrow.
	if !candidate.After(after) {
		candidate = candidate.AddDate(0, 0, 1)
	}

	// Scan up to 400 days to find a matching day.
	for i := 0; i < 400; i++ {
		if m.matchesDay(t, candidate) && m.inDateRange(t, candidate) {
			return &candidate
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	return nil // no future match found
}

func (m *ScheduledTaskManager) matchesDay(t *ScheduledTask, d time.Time) bool {
	if t.DayOfMonth > 0 && d.Day() != t.DayOfMonth {
		return false
	}
	if t.DayOfWeek >= 0 && int(d.Weekday()) != t.DayOfWeek {
		return false
	}
	return true
}

func (m *ScheduledTaskManager) inDateRange(t *ScheduledTask, d time.Time) bool {
	day := d.Format("2006-01-02")
	if t.StartDate != "" && day < t.StartDate {
		return false
	}
	if t.EndDate != "" && day > t.EndDate {
		return false
	}
	return true
}

func (m *ScheduledTaskManager) isExpired(t *ScheduledTask, now time.Time) bool {
	if t.EndDate == "" {
		return false
	}
	today := now.Format("2006-01-02")
	return today > t.EndDate
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

func (m *ScheduledTaskManager) load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("scheduled_task: read %s: %w", m.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &m.tasks)
}

func (m *ScheduledTaskManager) save() error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("scheduled_task: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(m.tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("scheduled_task: marshal: %w", err)
	}
	// Atomic write: write to temp file then rename to avoid corruption on crash.
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("scheduled_task: write tmp: %w", err)
	}
	return os.Rename(tmp, m.path)
}

// truncateStr truncates s to maxLen runes.
func truncateStr(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen]) + "..."
}

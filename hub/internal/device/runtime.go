package device

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

var ErrMachineOffline = errors.New("machine is offline")

type MachineRepository interface {
	GetByID(ctx context.Context, id string) (*store.Machine, error)
	ListByUserID(ctx context.Context, userID string) ([]*store.Machine, error)
	UpdateStatus(ctx context.Context, machineID string, status string) error
	UpdateHeartbeat(ctx context.Context, machineID string, at time.Time) error
}

type Runtime struct {
	mu sync.RWMutex

	desktopsByMachine map[string]*ws.ConnContext
	events            []MachineEvent
}

type Service struct {
	repo    MachineRepository
	runtime *Runtime
}

func (s *Service) IsMachineOnline(machineID string) bool {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()
	conn := s.runtime.desktopsByMachine[machineID]
	return conn != nil && conn.Conn != nil
}

type MachineRuntimeInfo struct {
	MachineID  string     `json:"machine_id"`
	UserID     string     `json:"user_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Platform   string     `json:"platform,omitempty"`
	Status     string     `json:"status,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	Role       string     `json:"role,omitempty"`
	Online     bool       `json:"online"`
}

type MachineEvent struct {
	Timestamp int64  `json:"timestamp"`
	MachineID string `json:"machine_id"`
	UserID    string `json:"user_id,omitempty"`
	Type      string `json:"type"`
	Message   string `json:"message,omitempty"`
}

func NewRuntime() *Runtime {
	return &Runtime{
		desktopsByMachine: map[string]*ws.ConnContext{},
		events:            make([]MachineEvent, 0, 128),
	}
}

func NewService(repo MachineRepository, runtime *Runtime) *Service {
	return &Service{repo: repo, runtime: runtime}
}

func (s *Service) BindDesktop(machineID string, ctx *ws.ConnContext) {
	s.runtime.mu.Lock()
	s.runtime.desktopsByMachine[machineID] = ctx
	s.appendEventLocked(MachineEvent{
		Timestamp: time.Now().Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(ctx),
		Type:      "bind",
		Message:   "machine websocket bound",
	})
	s.runtime.mu.Unlock()
}

func (s *Service) UnbindDesktop(ctx context.Context, machineID string, conn *ws.ConnContext) error {
	s.runtime.mu.Lock()
	current := s.runtime.desktopsByMachine[machineID]
	if current == conn || conn == nil {
		delete(s.runtime.desktopsByMachine, machineID)
		s.appendEventLocked(MachineEvent{
			Timestamp: time.Now().Unix(),
			MachineID: machineID,
			UserID:    safeConnUserID(current),
			Type:      "unbind",
			Message:   "machine websocket unbound",
		})
	}
	s.runtime.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	return s.repo.UpdateStatus(ctx, machineID, "offline")
}

func (s *Service) MarkOnline(ctx context.Context, machineID string) error {
	s.runtime.mu.Lock()
	conn := s.runtime.desktopsByMachine[machineID]
	s.appendEventLocked(MachineEvent{
		Timestamp: time.Now().Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(conn),
		Type:      "online",
		Message:   "machine marked online",
	})
	s.runtime.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	if err := s.repo.UpdateStatus(ctx, machineID, "online"); err != nil {
		return err
	}
	return s.repo.UpdateHeartbeat(ctx, machineID, time.Now())
}

func (s *Service) Heartbeat(ctx context.Context, machineID string) error {
	s.runtime.mu.Lock()
	conn := s.runtime.desktopsByMachine[machineID]
	s.appendEventLocked(MachineEvent{
		Timestamp: time.Now().Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(conn),
		Type:      "heartbeat",
		Message:   "machine heartbeat received",
	})
	s.runtime.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	return s.repo.UpdateHeartbeat(ctx, machineID, time.Now())
}

func (s *Service) SendToMachine(machineID string, msg any) error {
	s.runtime.mu.RLock()
	conn := s.runtime.desktopsByMachine[machineID]
	s.runtime.mu.RUnlock()
	if conn == nil || conn.Conn == nil {
		s.recordEvent(MachineEvent{
			Timestamp: time.Now().Unix(),
			MachineID: machineID,
			Type:      "send.failed",
			Message:   "machine offline during command dispatch",
		})
		return ErrMachineOffline
	}
	s.recordEvent(MachineEvent{
		Timestamp: time.Now().Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(conn),
		Type:      "send",
		Message:   "command dispatched to machine",
	})
	return conn.Conn.WriteJSON(msg)
}

func (s *Service) ListOnlineMachines() []MachineRuntimeInfo {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()

	out := make([]MachineRuntimeInfo, 0, len(s.runtime.desktopsByMachine))
	for machineID, conn := range s.runtime.desktopsByMachine {
		info := MachineRuntimeInfo{
			MachineID: machineID,
			Online:    true,
		}
		if conn != nil {
			info.UserID = conn.UserID
			info.Role = conn.Role
		}
		out = append(out, info)
	}
	return out
}

func (s *Service) ListMachines(ctx context.Context, userID string) ([]MachineRuntimeInfo, error) {
	if s.repo == nil {
		return s.ListOnlineMachines(), nil
	}

	items, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()

	out := make([]MachineRuntimeInfo, 0, len(items))
	for _, item := range items {
		info := MachineRuntimeInfo{
			MachineID:  item.ID,
			UserID:     item.UserID,
			Name:       item.Name,
			Platform:   item.Platform,
			Status:     item.Status,
			LastSeenAt: item.LastSeenAt,
		}
		if conn, ok := s.runtime.desktopsByMachine[item.ID]; ok && conn != nil && conn.Conn != nil {
			info.Role = conn.Role
			info.Online = true
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Service) ListEvents(limit int) []MachineEvent {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()

	if limit <= 0 || limit > len(s.runtime.events) {
		limit = len(s.runtime.events)
	}
	start := len(s.runtime.events) - limit
	if start < 0 {
		start = 0
	}

	out := make([]MachineEvent, 0, len(s.runtime.events)-start)
	for i := len(s.runtime.events) - 1; i >= start; i-- {
		out = append(out, s.runtime.events[i])
	}
	return out
}

func (s *Service) recordEvent(event MachineEvent) {
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	s.appendEventLocked(event)
}

func (s *Service) appendEventLocked(event MachineEvent) {
	s.runtime.events = append(s.runtime.events, event)
	if len(s.runtime.events) > 200 {
		s.runtime.events = s.runtime.events[len(s.runtime.events)-200:]
	}
}

func safeConnUserID(ctx *ws.ConnContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.UserID
}

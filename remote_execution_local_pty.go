package main

import "fmt"

type LocalPTYExecutionStrategy struct {
	newPTY func() PTYSession
}

func NewLocalPTYExecutionStrategy(newPTY func() PTYSession) *LocalPTYExecutionStrategy {
	return &LocalPTYExecutionStrategy{newPTY: newPTY}
}

func (s *LocalPTYExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	if s == nil || s.newPTY == nil {
		return nil, fmt.Errorf("local pty strategy is not configured")
	}

	pty := s.newPTY()
	if pty == nil {
		return nil, fmt.Errorf("local pty session is not available")
	}

	pid, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	return &PTYExecutionHandle{
		pid: pid,
		pty: pty,
	}, nil
}

type PTYExecutionHandle struct {
	pid int
	pty PTYSession
}

func (h *PTYExecutionHandle) PID() int {
	if h == nil {
		return 0
	}
	return h.pid
}

func (h *PTYExecutionHandle) Write(data []byte) error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Write(data)
}

func (h *PTYExecutionHandle) Interrupt() error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Interrupt()
}

func (h *PTYExecutionHandle) Kill() error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Kill()
}

func (h *PTYExecutionHandle) Output() <-chan []byte {
	if h == nil || h.pty == nil {
		return nil
	}
	return h.pty.Output()
}

func (h *PTYExecutionHandle) Exit() <-chan PTYExit {
	if h == nil || h.pty == nil {
		return nil
	}
	return h.pty.Exit()
}

func (h *PTYExecutionHandle) Close() error {
	if h == nil || h.pty == nil {
		return fmt.Errorf("execution handle is not available")
	}
	return h.pty.Close()
}

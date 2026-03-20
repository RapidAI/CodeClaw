package main

// ExecutionHandle represents a running remote execution instance.
type ExecutionHandle interface {
	PID() int
	Write(data []byte) error
	Interrupt() error
	Kill() error
	Output() <-chan []byte
	Exit() <-chan PTYExit
	Close() error
}

// ExecutionStrategy describes how a remote command is started and hosted.
type ExecutionStrategy interface {
	Start(cmd CommandSpec) (ExecutionHandle, error)
}

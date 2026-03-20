package remote

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ResolveExecutablePath validates and resolves a command path.
// It handles relative paths via LookPath, checks accessibility,
// and rejects directories.
func ResolveExecutablePath(execPath string) (string, error) {
	if !filepath.IsAbs(execPath) {
		resolved, err := exec.LookPath(execPath)
		if err != nil {
			return "", fmt.Errorf("command not found: %s (PATH contains %d entries): %w",
				execPath, len(strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))), err)
		}
		execPath = resolved
	}
	info, err := os.Stat(execPath)
	if err != nil {
		return "", fmt.Errorf("command not accessible: %s: %w", execPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("command is a directory: %s", execPath)
	}
	return execPath, nil
}

// ProcessPipes holds the stdin/stdout/stderr pipes for a child process.
type ProcessPipes struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
}

// CreateProcessPipes creates stdin, stdout, and stderr pipes on the given exec.Cmd.
func CreateProcessPipes(c *exec.Cmd) (ProcessPipes, error) {
	stdin, err := c.StdinPipe()
	if err != nil {
		return ProcessPipes{}, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return ProcessPipes{}, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return ProcessPipes{}, fmt.Errorf("stderr pipe: %w", err)
	}
	return ProcessPipes{Stdin: stdin, Stdout: stdout, Stderr: stderr}, nil
}

// ReaderCoordinator manages a set of reader goroutines that feed into
// a shared output channel. When all readers finish, the output channel
// is closed automatically.
type ReaderCoordinator struct {
	wg sync.WaitGroup
	ch chan []byte
}

// NewReaderCoordinator creates a coordinator with the given channel buffer size.
func NewReaderCoordinator(bufSize int) *ReaderCoordinator {
	return &ReaderCoordinator{
		ch: make(chan []byte, bufSize),
	}
}

// Add registers n reader goroutines.
func (rc *ReaderCoordinator) Add(n int) { rc.wg.Add(n) }

// Done marks one reader goroutine as finished.
func (rc *ReaderCoordinator) Done() { rc.wg.Done() }

// Output returns the shared output channel.
func (rc *ReaderCoordinator) Output() chan []byte { return rc.ch }

// CloseWhenDone starts a goroutine that waits for all readers to finish,
// then closes the output channel.
func (rc *ReaderCoordinator) CloseWhenDone() {
	go func() {
		rc.wg.Wait()
		close(rc.ch)
	}()
}

// Wait blocks until all registered reader goroutines have called Done.
func (rc *ReaderCoordinator) Wait() { rc.wg.Wait() }

// BuildEnvList merges the current process environment with additional env vars.
// Keys in env override existing environment variables. The result is sorted.
func BuildEnvList(env map[string]string) []string {
	base := os.Environ()
	merged := make(map[string]string, len(base)+len(env))
	for _, item := range base {
		if k, v, ok := strings.Cut(item, "="); ok {
			merged[k] = v
		}
	}
	for key, value := range env {
		merged[key] = value
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, key+"="+merged[key])
	}
	return items
}

// BuildExecCmd creates an *exec.Cmd with standard configuration:
// resolved path, args, working directory, merged environment, and
// hidden console window (Windows).
func BuildExecCmd(execPath string, args []string, cwd string, env map[string]string) *exec.Cmd {
	c := exec.Command(execPath, args...)
	c.Dir = cwd
	c.Env = BuildEnvList(env)
	HideCommandWindow(c)
	return c
}

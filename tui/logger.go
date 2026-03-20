package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// TUILogger 实现 corelib.Logger 接口，输出到 stderr。
// TUI 模式下日志写入 stderr 避免干扰 Bubble Tea 渲染。
type TUILogger struct {
	mu      sync.Mutex
	logFile *os.File // 可选的日志文件
}

// NewTUILogger 创建 TUI 日志实例。
func NewTUILogger() *TUILogger {
	return &TUILogger{}
}

// SetLogFile 设置日志文件输出（daemon 模式使用）。
func (l *TUILogger) SetLogFile(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	l.logFile = f
	return nil
}

func (l *TUILogger) log(level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] %s: %s\n", ts, level, msg)

	if l.logFile != nil {
		l.logFile.WriteString(line)
	} else {
		fmt.Fprint(os.Stderr, line)
	}
}

func (l *TUILogger) Debug(format string, args ...interface{}) { l.log("DBG", format, args...) }
func (l *TUILogger) Info(format string, args ...interface{})  { l.log("INF", format, args...) }
func (l *TUILogger) Warn(format string, args ...interface{})  { l.log("WRN", format, args...) }
func (l *TUILogger) Error(format string, args ...interface{}) { l.log("ERR", format, args...) }

// Close 关闭日志文件。
func (l *TUILogger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logFile != nil {
		l.logFile.Close()
		l.logFile = nil
	}
}

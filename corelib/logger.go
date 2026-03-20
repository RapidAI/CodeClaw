// Package corelib 是 MaClaw 内核库，包含所有与 UI 无关的核心业务逻辑。
// GUI 和 TUI/CLI 层通过引用此包来驱动业务功能。
package corelib

import (
	"log"
	"os"
)

// Logger 定义内核日志接口。
// GUI 可提供写入文件的实现，TUI 可提供状态栏输出的实现，
// CLI 可使用默认的 stderr 输出。
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// DefaultLogger 使用标准库 log 输出到 stderr。
type DefaultLogger struct {
	logger *log.Logger
}

// NewDefaultLogger 创建一个输出到 stderr 的默认 Logger。
func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{
		logger: log.New(os.Stderr, "", log.LstdFlags),
	}
}

func (l *DefaultLogger) Debug(msg string, args ...interface{}) {
	l.logger.Printf("[DEBUG] "+msg, args...)
}

func (l *DefaultLogger) Info(msg string, args ...interface{}) {
	l.logger.Printf("[INFO]  "+msg, args...)
}

func (l *DefaultLogger) Warn(msg string, args ...interface{}) {
	l.logger.Printf("[WARN]  "+msg, args...)
}

func (l *DefaultLogger) Error(msg string, args ...interface{}) {
	l.logger.Printf("[ERROR] "+msg, args...)
}

// Ensure DefaultLogger implements Logger.
var _ Logger = (*DefaultLogger)(nil)

package corelib

import "sync"

// EventHandler 是事件回调函数类型。
type EventHandler func(payload interface{})

// EventEmitter 定义内核事件分发接口。
// GUI 层实现此接口将事件转发为 Wails runtime.EventsEmit 调用。
// TUI 层实现此接口将事件转换为 Bubble Tea 的 Msg。
type EventEmitter interface {
	// Emit 触发一个事件。实现必须保证不阻塞调用方（异步分发）。
	Emit(eventType string, payload interface{})

	// Subscribe 注册事件监听器。支持同一事件多个监听器。
	Subscribe(eventType string, handler EventHandler)
}

// ChannelEmitter 是基于 Go channel 的默认 EventEmitter 实现。
// 使用带缓冲的 channel 异步分发事件，监听器 panic 会被捕获并记录日志。
type ChannelEmitter struct {
	mu       sync.RWMutex
	handlers map[string][]EventHandler
	logger   Logger
	bufSize  int
}

// NewChannelEmitter 创建一个基于 channel 的事件分发器。
func NewChannelEmitter(logger Logger, bufSize int) *ChannelEmitter {
	if bufSize <= 0 {
		bufSize = 256
	}
	return &ChannelEmitter{
		handlers: make(map[string][]EventHandler),
		logger:   logger,
		bufSize:  bufSize,
	}
}

func (e *ChannelEmitter) Emit(eventType string, payload interface{}) {
	e.mu.RLock()
	handlers := e.handlers[eventType]
	e.mu.RUnlock()

	for _, h := range handlers {
		h := h
		go e.safeCall(eventType, h, payload)
	}
}

func (e *ChannelEmitter) Subscribe(eventType string, handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[eventType] = append(e.handlers[eventType], handler)
}

func (e *ChannelEmitter) safeCall(eventType string, handler EventHandler, payload interface{}) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("event handler panic on %q: %v", eventType, r)
		}
	}()
	handler(payload)
}

// NoopEmitter 是空实现，用于纯 CLI 批处理模式或不需要事件通知的场景。
type NoopEmitter struct{}

func (NoopEmitter) Emit(string, interface{})       {}
func (NoopEmitter) Subscribe(string, EventHandler) {}

// Ensure implementations satisfy EventEmitter.
var _ EventEmitter = (*ChannelEmitter)(nil)
var _ EventEmitter = NoopEmitter{}

package main

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	tea "github.com/charmbracelet/bubbletea"
)

// BubbleTeaEventBridge 将 corelib.EventEmitter 事件转发为 tea.Msg。
// 实现 corelib.EventEmitter 接口。
type BubbleTeaEventBridge struct {
	mu       sync.RWMutex
	handlers map[string][]corelib.EventHandler
	program  *tea.Program // 绑定后才能转发
}

// NewBubbleTeaEventBridge 创建事件桥接器。
func NewBubbleTeaEventBridge() *BubbleTeaEventBridge {
	return &BubbleTeaEventBridge{
		handlers: make(map[string][]corelib.EventHandler),
	}
}

// SetProgram 绑定 Bubble Tea Program，使事件可以通过 Send 转发到 TUI。
func (b *BubbleTeaEventBridge) SetProgram(p *tea.Program) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.program = p
}

// Emit 实现 corelib.EventEmitter 接口。
// 同时触发已注册的 handler 和向 Bubble Tea 发送消息。
func (b *BubbleTeaEventBridge) Emit(eventType string, data interface{}) {
	b.mu.RLock()
	handlers := b.handlers[eventType]
	p := b.program
	b.mu.RUnlock()

	// 触发已注册的 handler
	for _, h := range handlers {
		go func(fn corelib.EventHandler) {
			defer func() { recover() }()
			fn(data)
		}(h)
	}

	// 转发到 Bubble Tea
	if p != nil {
		p.Send(kernelEventMsg{eventType: eventType, data: data})
	}
}

// Subscribe 实现 corelib.EventEmitter 接口。
func (b *BubbleTeaEventBridge) Subscribe(eventType string, handler corelib.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

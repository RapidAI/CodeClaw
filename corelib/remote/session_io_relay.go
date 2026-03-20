package remote

import "sync"

// SessionIORelay manages multi-device IO relay for session roaming.
// Output is broadcast to all subscribed devices; input uses last-writer-wins.
type SessionIORelay struct {
	mu        sync.RWMutex
	listeners map[string]map[string]chan string // map[sessionID]map[deviceID]outputChannel
}

// NewSessionIORelay creates a new SessionIORelay instance.
func NewSessionIORelay() *SessionIORelay {
	return &SessionIORelay{
		listeners: make(map[string]map[string]chan string),
	}
}

// Subscribe registers a device to receive output for a session.
func (r *SessionIORelay) Subscribe(sessionID, deviceID string) <-chan string {
	r.mu.Lock()
	defer r.mu.Unlock()

	devices, ok := r.listeners[sessionID]
	if !ok {
		devices = make(map[string]chan string)
		r.listeners[sessionID] = devices
	}
	if old, exists := devices[deviceID]; exists {
		close(old)
	}
	ch := make(chan string, 64)
	devices[deviceID] = ch
	return ch
}

// Unsubscribe removes a device listener and closes its channel.
func (r *SessionIORelay) Unsubscribe(sessionID, deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	devices, ok := r.listeners[sessionID]
	if !ok {
		return
	}
	if ch, exists := devices[deviceID]; exists {
		close(ch)
		delete(devices, deviceID)
	}
	if len(devices) == 0 {
		delete(r.listeners, sessionID)
	}
}

// BroadcastOutput sends output to all subscribed devices for a session.
func (r *SessionIORelay) BroadcastOutput(sessionID, output string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	devices, ok := r.listeners[sessionID]
	if !ok {
		return
	}
	for _, ch := range devices {
		select {
		case ch <- output:
		default:
		}
	}
}

// ForwardInput returns the input as-is (last-writer-wins is implicit).
func (r *SessionIORelay) ForwardInput(sessionID, deviceID, input string) string {
	return input
}

// ListenerCount returns the number of active listeners for a session.
func (r *SessionIORelay) ListenerCount(sessionID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.listeners[sessionID])
}

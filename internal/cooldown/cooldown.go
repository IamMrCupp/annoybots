// Package cooldown tracks per-key cooldown windows so the bot doesn't repeat the
// same trigger or interjection too often in a given channel.
package cooldown

import (
	"sync"
	"time"
)

// Manager records the last time each key fired and reports readiness.
type Manager struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

// New returns a Manager backed by the real clock.
func New() *Manager { return NewWithClock(time.Now) }

// NewWithClock returns a Manager using the supplied clock (used in tests).
func NewWithClock(now func() time.Time) *Manager {
	return &Manager{last: make(map[string]time.Time), now: now}
}

// Ready reports whether key is past its cooldown window d without consuming it.
func (m *Manager) Ready(key string, d time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.last[key]
	if !ok {
		return true
	}
	return m.now().Sub(t) >= d
}

// Use atomically checks readiness and, if ready, marks the key as fired now.
// It returns true only when the action may proceed.
func (m *Manager) Use(key string, d time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if t, ok := m.last[key]; ok && now.Sub(t) < d {
		return false
	}
	m.last[key] = now
	return true
}

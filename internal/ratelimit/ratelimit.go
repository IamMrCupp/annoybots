// Package ratelimit provides a token-bucket limiter used to throttle outbound
// messages per network. Twitch in particular enforces strict per-channel rate
// limits (roughly 20 messages / 30s for non-moderators) and will disconnect a
// client that exceeds them, so every send passes through a limiter.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a token-bucket rate limiter safe for concurrent use.
type Limiter struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	rate   float64 // tokens replenished per second
	last   time.Time
	now    func() time.Time
}

// New returns a Limiter that allows bursts of up to burst messages and
// replenishes perSecond tokens each second.
func New(burst int, perSecond float64) *Limiter {
	return newWithClock(burst, perSecond, time.Now)
}

func newWithClock(burst int, perSecond float64, now func() time.Time) *Limiter {
	if burst < 1 {
		burst = 1
	}
	if perSecond <= 0 {
		perSecond = 1
	}
	return &Limiter{
		tokens: float64(burst),
		burst:  float64(burst),
		rate:   perSecond,
		last:   now(),
		now:    now,
	}
}

// refill adds tokens based on elapsed wall-clock time. Caller must hold mu.
func (l *Limiter) refill() {
	t := l.now()
	if elapsed := t.Sub(l.last).Seconds(); elapsed > 0 {
		l.tokens += elapsed * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = t
	}
}

// Allow reports whether a token is available, consuming it if so. It never
// blocks, making it suitable for drop-on-overflow decisions.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// Reserve consumes a token and returns how long the caller should wait before
// the associated action may proceed. A return of 0 means the action may happen
// immediately. Callers that block on the returned duration get smooth pacing.
func (l *Limiter) Reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	l.tokens--
	if l.tokens >= 0 {
		return 0
	}
	deficit := -l.tokens
	return time.Duration(deficit / l.rate * float64(time.Second))
}

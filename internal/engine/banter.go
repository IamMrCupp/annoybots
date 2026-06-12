package engine

import (
	"strings"
	"sync"
	"time"
)

// isSibling reports whether nick belongs to another known bot.
func (e *Engine) isSibling(nick string) bool {
	return e.siblings[strings.ToLower(nick)]
}

// IsSibling reports whether nick belongs to another known bot. Used by the
// coordinator to avoid letting bots trigger skits off each other.
func (e *Engine) IsSibling(nick string) bool { return e.isSibling(nick) }

// maybeBanter optionally fires a controlled reply to a sibling bot. It is the
// ONLY way the bot reacts to another bot: normal triggers and interjections are
// suppressed for sibling messages (see Handle), so the only cross-talk path is
// this one, which is hard-bounded against runaway loops by both a per-channel
// cooldown and a windowed reply cap.
func (e *Engine) maybeBanter(msg Message, out Sender) {
	b := e.p.Banter
	if !b.Enabled || msg.Private {
		return
	}
	if !e.roll(b.Chance) {
		return
	}
	key := msg.Network + ":" + msg.Channel
	if b.Cooldown.D() > 0 && !e.cool.Use("banter:"+key, b.Cooldown.D()) {
		return
	}
	if b.MaxPerWindow > 0 && !e.banter.allow(key, b.MaxPerWindow, b.Window.D(), e.now()) {
		return
	}
	line := e.render(e.pick(b.Lines), msg, nil)
	if line == "" {
		return
	}
	e.emit(out, msg.Network, msg.Channel, line, b.Action)
}

// windowCounter enforces "at most N events per rolling window" per key. It is
// the second, independent safety net behind banter cooldowns: even if both bots
// were misconfigured with tiny cooldowns, each can only emit MaxPerWindow banter
// lines per Window, so total channel volume is strictly bounded.
type windowCounter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newWindowCounter() *windowCounter {
	return &windowCounter{hits: make(map[string][]time.Time)}
}

// allow records an event at now and returns false if the key already has limit
// events within the trailing window.
func (w *windowCounter) allow(key string, limit int, window time.Duration, now time.Time) bool {
	if limit <= 0 {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	cut := now.Add(-window)
	kept := w.hits[key][:0]
	for _, t := range w.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		w.hits[key] = kept
		return false
	}
	w.hits[key] = append(kept, now)
	return true
}

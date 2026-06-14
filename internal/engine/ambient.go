package engine

import (
	"strings"
	"time"
)

// activitySep separates network from channel in the activity map key. A NUL byte
// can't appear in a network name or channel, so it's an unambiguous joiner.
const activitySep = "\x00"

// recordActivity timestamps the most recent human activity in a channel.
func (e *Engine) recordActivity(network, channel string) {
	e.amu.Lock()
	e.activity[network+activitySep+channel] = e.now()
	e.amu.Unlock()
}

// AmbientEnabled reports whether the self-initiated ambient timer is configured on.
func (e *Engine) AmbientEnabled() bool { return e.p.AmbientTimer.Enabled }

// AmbientInterval is how often the caller should invoke Tick (already defaulted).
func (e *Engine) AmbientInterval() time.Duration { return e.p.AmbientTimer.Interval.D() }

// chanRef identifies one channel on one network.
type chanRef struct{ network, channel string }

// Tick self-initiates ambient chatter into channels that have recent-but-now-quiet
// activity — the BMotion "the bot sometimes just talks on its own" behavior. The
// caller drives it on a timer (see AmbientInterval). Each eligible channel gets at
// most one line per tick, gated by a per-channel cooldown, so it can't spam; dead
// or never-active channels are skipped entirely.
func (e *Engine) Tick(out Sender) {
	at := e.p.AmbientTimer
	if !at.Enabled {
		return
	}
	for _, ch := range e.eligibleChannels(at, e.now()) {
		if !e.roll(at.Chance) {
			continue
		}
		key := "timer:" + ch.network + activitySep + ch.channel
		if at.Cooldown.D() > 0 && !e.cool.Use(key, at.Cooldown.D()) {
			continue
		}
		e.emitAmbient(ch.network, ch.channel, out)
	}
}

// eligibleChannels returns channels whose last human activity is inside the
// window [QuietFor, ActiveWithin] ago: long enough ago to be a lull worth
// breaking, recent enough that the channel isn't dead. Snapshots under the lock
// so Tick can roll/emit without holding it.
func (e *Engine) eligibleChannels(at AmbientTimer, now time.Time) []chanRef {
	e.amu.Lock()
	defer e.amu.Unlock()
	var refs []chanRef
	for k, last := range e.activity {
		since := now.Sub(last)
		if at.QuietFor.D() > 0 && since < at.QuietFor.D() {
			continue // still active — don't talk over an ongoing conversation
		}
		if at.ActiveWithin.D() > 0 && since > at.ActiveWithin.D() {
			continue // dead too long — leave it be
		}
		if net, chn, ok := splitActivityKey(k); ok {
			refs = append(refs, chanRef{net, chn})
		}
	}
	return refs
}

// emitAmbient sends one self-initiated line into a channel, reusing the same
// pools as the reactive paths: an interjection (optionally Markov), else a quote.
func (e *Engine) emitAmbient(network, channel string, out Sender) {
	m := Message{Network: network, Channel: channel, Self: e.p.Name}
	if e.p.Interjections.Enabled {
		line := ""
		if e.p.Interjections.UseMarkov && e.p.Markov.Enabled {
			line = e.markovLine()
		}
		if line == "" && len(e.p.Interjections.Lines) > 0 {
			line = e.render(e.pick(e.p.Interjections.Lines), m, nil)
		}
		if line != "" {
			out.Say(network, channel, line)
			return
		}
	}
	if e.p.Quotes.Enabled {
		if line := e.render(e.pick(e.allQuotes()), m, nil); line != "" {
			out.Say(network, channel, line)
		}
	}
}

func splitActivityKey(k string) (network, channel string, ok bool) {
	network, channel, ok = strings.Cut(k, activitySep)
	return
}

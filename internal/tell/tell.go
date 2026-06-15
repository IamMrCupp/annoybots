// Package tell implements "leave a message for someone" — a public channel
// command, !message <nick> <text>, delivered when that nick next speaks in or
// JOINs the channel. It is the modern eggdrop ".note"/tell, riding on the F1
// event dispatcher's JOIN hook plus the normal message stream.
package tell

import (
	"strings"
	"sync"
	"time"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/event"
)

const (
	maxPerTarget = 10  // cap an inbox so it can't be flooded
	maxTextLen   = 300 // trim overlong notes
)

type note struct {
	from string
	text string
	at   time.Time
}

// Manager stores pending notes per (network, channel, target) and delivers them.
type Manager struct {
	mu      sync.Mutex
	pending map[string][]note
	out     engine.Sender
	now     func() time.Time
}

// New returns a Manager that delivers via out.
func New(out engine.Sender) *Manager {
	return &Manager{pending: make(map[string][]note), out: out, now: time.Now}
}

func key(network, channel, nick string) string {
	return strings.ToLower(network) + "|" + strings.ToLower(channel) + "|" + strings.ToLower(nick)
}

// Handle processes a channel message: it first delivers any notes waiting for the
// speaker (so a note lands the moment they say anything), then stores a new note
// if the message is "!message <nick> <text>". Returns true only when it consumed
// a !message command, so normal chatter still flows to the engine.
func (m *Manager) Handle(msg engine.Message) bool {
	if msg.Private {
		return false
	}
	m.deliver(msg.Network, msg.Channel, msg.Nick)

	fields := strings.Fields(msg.Text)
	if len(fields) == 0 || strings.ToLower(fields[0]) != "!message" {
		return false
	}
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !message <nick> <text>")
		return true
	}
	target := fields[1]
	text := strings.Join(fields[2:], " ")
	if len(text) > maxTextLen {
		text = text[:maxTextLen]
	}

	k := key(msg.Network, msg.Channel, target)
	m.mu.Lock()
	if len(m.pending[k]) >= maxPerTarget {
		m.mu.Unlock()
		m.out.Say(msg.Network, msg.Channel, target+"'s inbox is full; try again later.")
		return true
	}
	m.pending[k] = append(m.pending[k], note{from: msg.Nick, text: text, at: m.now()})
	m.mu.Unlock()

	m.out.Say(msg.Network, msg.Channel, "ok "+msg.Nick+", i'll tell "+target+" when they turn up.")
	return true
}

// OnJoin delivers any pending notes when the target joins the channel.
func (m *Manager) OnJoin(ev event.Event) {
	if ev.Kind != event.Join {
		return
	}
	m.deliver(ev.Network, ev.Channel, ev.Nick)
}

// deliver flushes and clears every note waiting for nick in this channel.
func (m *Manager) deliver(network, channel, nick string) {
	k := key(network, channel, nick)
	m.mu.Lock()
	notes := m.pending[k]
	delete(m.pending, k)
	m.mu.Unlock()
	for _, n := range notes {
		m.out.Say(network, channel, nick+": "+n.from+" wanted you to know — "+n.text)
	}
}

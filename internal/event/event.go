// Package event provides a transport-agnostic event/hook dispatcher — the
// eggdrop-style "bind" system. Transports emit normalized Events (a user joined,
// parted, quit, changed mode, …) and features subscribe to the kinds they care
// about. It is the foundation the tell/memo, channel-keeping, games, and plugin
// layers build on; today the engine only ever saw PRIVMSG, so the bot was blind
// to everything else happening in a channel.
package event

import (
	"strings"
	"sync"
)

// Kind enumerates the event types a transport can emit.
type Kind int

const (
	Message Kind = iota // a channel/private message (PRIVMSG)
	Join                // someone joined a channel
	Part                // someone left a channel
	Quit                // someone disconnected from the network
	Nick                // someone changed nick
	Mode                // a channel/user mode change
	Kick                // someone was kicked from a channel
)

// String renders the kind for logs.
func (k Kind) String() string {
	switch k {
	case Message:
		return "message"
	case Join:
		return "join"
	case Part:
		return "part"
	case Quit:
		return "quit"
	case Nick:
		return "nick"
	case Mode:
		return "mode"
	case Kick:
		return "kick"
	default:
		return "unknown"
	}
}

// Event is a normalized thing-that-happened on some network, deliberately
// independent of any IRC/Discord library so handlers stay transport-agnostic.
// Which fields are populated depends on Kind (e.g. Quit has no Channel).
type Event struct {
	Kind    Kind
	Network string // logical network name
	Channel string // channel involved (Join/Part/Mode/Kick); empty for Quit
	Nick    string // the subject (who joined/parted/quit/…)
	Ident   string // IRC username/ident, when known
	Host    string // IRC hostname/cloak, when known
	Account string // verified services account, when known
	Actor   string // who performed it, when distinct from Nick (kicker, mode-setter)
	Text    string // free text: message body, mode string, kick reason, new nick
}

// Handler reacts to an Event.
type Handler func(Event)

// Dispatcher fans Events out to handlers subscribed by Kind. Safe for concurrent
// Emit and On (transports emit from their own goroutines).
type Dispatcher struct {
	mu   sync.RWMutex
	subs map[Kind][]Handler
}

// New returns an empty Dispatcher.
func New() *Dispatcher {
	return &Dispatcher{subs: make(map[Kind][]Handler)}
}

// On subscribes h to every Event of the given kind.
func (d *Dispatcher) On(k Kind, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subs[k] = append(d.subs[k], h)
}

// Emit delivers e to all handlers subscribed to e.Kind. Handlers run synchronously
// on the caller's goroutine; the subscriber list is snapshotted first so a handler
// may itself subscribe without deadlocking.
func (d *Dispatcher) Emit(e Event) {
	d.mu.RLock()
	hs := d.subs[e.Kind]
	snapshot := make([]Handler, len(hs))
	copy(snapshot, hs)
	d.mu.RUnlock()
	for _, h := range snapshot {
		h(e)
	}
}

// SplitUserHost splits an IRC source prefix "nick!ident@host" into its ident and
// host parts. Missing parts come back empty. Exposed for reuse by transports.
func SplitUserHost(source string) (ident, host string) {
	if i := strings.IndexByte(source, '!'); i >= 0 {
		source = source[i+1:]
	} else {
		return "", ""
	}
	if at := strings.IndexByte(source, '@'); at >= 0 {
		return source[:at], source[at+1:]
	}
	return source, ""
}

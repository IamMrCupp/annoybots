// Package chanops adds the eggdrop-style in-channel !op command: a recognized
// admin types "!op" and whichever bot in the channel currently holds channel
// operator grants them +o. Authorization reuses the bot's F2 flag system; the
// granting is done by whatever transport can (IRC), so it's a no-op off-IRC.
package chanops

import (
	"log/slog"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Opper grants channel operator status, returning true only when the mode was
// actually sent (i.e. this bot held ops). bot.Router satisfies this.
type Opper interface {
	Op(network, channel, nick string) bool
}

// Manager handles the !op command for one bot.
type Manager struct {
	op    Opper
	out   engine.Sender
	authz func(engine.Message) bool // is the sender a recognized admin? (nil = nobody)
	log   *slog.Logger
}

// New builds a Manager. op grants the mode (nil-safe), out sends the DM hint.
func New(op Opper, out engine.Sender, log *slog.Logger) *Manager {
	return &Manager{op: op, out: out, log: log}
}

// SetAuthz wires the admin-authorization predicate. Without it, !op does nothing.
func (m *Manager) SetAuthz(fn func(engine.Message) bool) { m.authz = fn }

func (m *Manager) authorized(msg engine.Message) bool {
	return m.authz != nil && m.authz(msg)
}

// Handle consumes an "!op" command. It returns true whenever the message is an
// !op command (so it never falls through to the engine), regardless of whether
// this particular bot acted.
//
// Multi-bot coordination is implicit: every bot present sees the "!op", but only
// the one(s) actually holding ops send the mode — the rest consume it and stay
// silent. Unauthorized senders are ignored silently too, so a channel full of
// bots produces neither a mode war nor a notice-storm; the +o itself is the ack.
func (m *Manager) Handle(msg engine.Message) bool {
	if msg.Text == "" {
		return false
	}
	fields := strings.Fields(msg.Text)
	if !strings.EqualFold(fields[0], "!op") {
		return false
	}
	// From here on it's an !op command — always consumed.
	if msg.Private {
		m.notice(msg, "use !op in a channel where a bot holds operator status.")
		return true
	}
	if !m.authorized(msg) {
		return true // silent: don't advertise the command or leak the admin list
	}
	if m.op != nil && m.op.Op(msg.Network, msg.Channel, msg.Nick) {
		m.log.Info("op: granted via !op", "network", msg.Network, "channel", msg.Channel, "nick", msg.Nick)
	}
	return true
}

// notice sends a private NOTICE to the sender when the transport supports one,
// else a plain message.
func (m *Manager) notice(msg engine.Message, text string) {
	if m.out == nil {
		return
	}
	if n, ok := m.out.(engine.Noticer); ok {
		n.Notice(msg.Network, msg.Nick, text)
		return
	}
	m.out.Say(msg.Network, msg.Nick, text)
}

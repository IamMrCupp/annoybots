// Package chanops adds the eggdrop-style in-channel op console: a recognized
// admin types !op / !deop / !voice / !devoice / !kick and whichever bot in the
// channel currently holds channel operator carries it out. Authorization reuses
// the bot's F2 flag system; the modes are sent by whatever transport can (IRC),
// so it's a no-op off-IRC.
package chanops

import (
	"log/slog"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Opper applies channel modes, returning true only when the change was actually
// sent (i.e. this bot held ops). bot.Router satisfies this.
type Opper interface {
	Op(network, channel, nick string) bool
	Mode(network, channel, modes, nick string) bool
	Kick(network, channel, nick, reason string) bool
}

// Manager handles the channel-op commands for one bot.
type Manager struct {
	op       Opper
	out      engine.Sender
	authz    func(engine.Message) bool // is the sender a recognized admin? (nil = nobody)
	siblings func(string) bool         // is this nick one of our own bots?
	log      *slog.Logger
}

// New builds a Manager. op applies the modes (nil-safe), out sends the DM hint.
func New(op Opper, out engine.Sender, log *slog.Logger) *Manager {
	return &Manager{op: op, out: out, log: log}
}

// SetAuthz wires the admin-authorization predicate. Without it, nothing works.
func (m *Manager) SetAuthz(fn func(engine.Message) bool) { m.authz = fn }

// SetSiblings wires the "is this one of our bots?" check, so the console can
// refuse to deop or kick the very bots that hold the channel.
func (m *Manager) SetSiblings(fn func(string) bool) { m.siblings = fn }

func (m *Manager) authorized(msg engine.Message) bool {
	return m.authz != nil && m.authz(msg)
}

// protected reports whether a nick is this bot or one of its siblings — never a
// valid target for a deop or a kick.
func (m *Manager) protected(msg engine.Message, nick string) bool {
	if strings.EqualFold(nick, msg.Self) {
		return true
	}
	return m.siblings != nil && m.siblings(nick)
}

// Handle consumes a channel-op command. It returns true whenever the message is
// one (so it never falls through to the engine), regardless of whether this bot
// acted.
//
// Multi-bot coordination is implicit: every bot present sees the command, but
// only the one(s) actually holding ops send anything — the rest consume it and
// stay silent. Unauthorized senders are ignored silently too, so a channel full
// of bots produces neither a mode war nor a notice-storm.
func (m *Manager) Handle(msg engine.Message) bool {
	if msg.Text == "" {
		return false
	}
	fields := strings.Fields(msg.Text)
	verb := strings.ToLower(fields[0])
	modes, ok := modeFor(verb)
	if !ok && verb != "!kick" {
		return false
	}
	// From here on it's a channel-op command — always consumed.
	if msg.Private {
		m.notice(msg, "use "+verb+" in a channel where a bot holds operator status.")
		return true
	}
	if !m.authorized(msg) {
		return true // silent: don't advertise the console or leak the admin list
	}
	if m.op == nil {
		return true
	}

	if verb == "!kick" {
		if len(fields) < 2 {
			m.notice(msg, "usage: !kick <nick> [reason]")
			return true
		}
		target := fields[1]
		if m.protected(msg, target) {
			m.notice(msg, "refusing to kick "+target+" — that's one of the bots holding this channel.")
			return true
		}
		reason := strings.Join(fields[2:], " ")
		if reason == "" {
			reason = "requested by " + msg.Nick
		}
		if m.op.Kick(msg.Network, msg.Channel, target, reason) {
			m.log.Info("chanops: kicked", "channel", msg.Channel, "nick", target, "by", msg.Nick)
		}
		return true
	}

	// op/deop/voice/devoice default to the sender when no nick is given.
	target := msg.Nick
	if len(fields) >= 2 {
		target = fields[1]
	}
	if strings.HasPrefix(modes, "-") && m.protected(msg, target) {
		m.notice(msg, "refusing to "+strings.TrimPrefix(verb, "!")+" "+target+" — that's one of the bots holding this channel.")
		return true
	}
	if m.op.Mode(msg.Network, msg.Channel, modes, target) {
		m.log.Info("chanops: mode", "channel", msg.Channel, "modes", modes, "nick", target, "by", msg.Nick)
	}
	return true
}

// modeFor maps a command verb to its channel mode change.
func modeFor(verb string) (string, bool) {
	switch verb {
	case "!op":
		return "+o", true
	case "!deop":
		return "-o", true
	case "!voice":
		return "+v", true
	case "!devoice":
		return "-v", true
	}
	return "", false
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

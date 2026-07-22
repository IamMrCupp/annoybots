// Package irc adapts the ergochat/irc-go event client into a multi-network
// manager. One process holds many simultaneous connections (real IRC networks,
// a private InspIRCd test net, and Twitch) and routes every inbound channel
// message through a single handler, while pacing outbound messages per network.
package irc

import (
	"context"
	"crypto/tls"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"

	"github.com/IamMrCupp/annoybots/internal/config"
	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/event"
	"github.com/IamMrCupp/annoybots/internal/ratelimit"
)

var twitchCaps = []string{"twitch.tv/tags", "twitch.tv/commands", "twitch.tv/membership"}

// Handler receives normalized inbound messages. Replies are sent via the Router
// (which the engine is given), not directly, so cross-transport routing works.
type Handler func(engine.Message)

type outMsg struct {
	target string
	text   string
	action bool
	notice bool
}

// conn is a single network connection plus its outbound pacing.
type conn struct {
	cfg          config.Network
	ic           *ircevent.Connection
	limiter      *ratelimit.Limiter
	out          chan outMsg
	log          *slog.Logger
	nickservPass string      // if set, IDENTIFY to NickServ on connect (non-SASL networks)
	keeper       *chankeeper // if set, eggdrop-style channel keeping (auto-op protected nicks)
}

// Manager owns all connections and implements engine.Sender.
type Manager struct {
	conns   map[string]*conn
	order   []string
	handler Handler
	emit    func(event.Event) // presence/event sink (default no-op; set via SetEventSink)
	log     *slog.Logger
	wg      sync.WaitGroup
}

// SetEventSink wires JOIN/PART/QUIT (and future IRC events) to a dispatcher.
func (m *Manager) SetEventSink(fn func(event.Event)) {
	if fn != nil {
		m.emit = fn
	}
}

// EnableChanKeep turns on eggdrop-style channel keeping on every IRC connection:
// once opped, the bot keeps the protect nicks (typically the sibling bots) opped.
func (m *Manager) EnableChanKeep(protect []string) {
	for _, c := range m.conns {
		cc := c
		send := func(channel, modes, arg string) {
			if err := cc.ic.Send("MODE", channel, modes, arg); err != nil {
				cc.log.Warn("chankeep mode failed", "channel", channel, "err", err)
			}
		}
		cc.keeper = newChankeeper(protect, cc.ic.CurrentNick, send, cc.log)
	}
}

// NewManager builds (but does not start) connections for every network. getenv
// resolves secret env var names to their values (usually os.Getenv).
func NewManager(nets []config.Network, handler Handler, log *slog.Logger, getenv func(string) string) (*Manager, error) {
	m := &Manager{conns: make(map[string]*conn), handler: handler, emit: func(event.Event) {}, log: log}
	for _, n := range nets {
		c := newConn(n, log, getenv)
		m.bind(c)
		m.conns[n.Name] = c
		m.order = append(m.order, n.Name)
	}
	return m, nil
}

func newConn(n config.Network, log *slog.Logger, getenv func(string) string) *conn {
	ic := &ircevent.Connection{
		Server:        n.Server,
		Nick:          n.Nick,
		User:          n.User,
		RealName:      n.RealName,
		UseTLS:        n.TLS,
		QuitMessage:   "reborn, and gone again",
		Timeout:       60 * time.Second,
		KeepAlive:     4 * time.Minute,
		ReconnectFreq: 30 * time.Second,
	}
	if n.TLS {
		ic.TLSConfig = &tls.Config{
			ServerName:         hostOnly(n.Server),
			InsecureSkipVerify: n.InsecureSkipVerify, //nolint:gosec // opt-in for self-signed test nets
		}
	}
	if n.PasswordEnv != "" {
		ic.Password = getenv(n.PasswordEnv)
	}

	if n.Kind == "twitch" {
		// Twitch requires a lowercase nick, an "oauth:" password, and CAP
		// negotiation for tags/commands/membership.
		ic.Nick = strings.ToLower(n.Nick)
		ic.User = ic.Nick
		ic.RequestCaps = append(ic.RequestCaps, twitchCaps...)
		if ic.Password != "" && !strings.HasPrefix(ic.Password, "oauth:") {
			ic.Password = "oauth:" + ic.Password
		}
	} else {
		ic.RequestCaps = append(ic.RequestCaps, "message-tags", "server-time", "account-tag")
		if n.SASL {
			ic.SASLLogin = n.SASLUser
			ic.SASLPassword = getenv(n.SASLPassEnv)
		}
	}

	c := &conn{
		cfg:     n,
		ic:      ic,
		limiter: ratelimit.New(n.Rate.Burst, n.Rate.PerSecond),
		out:     make(chan outMsg, 256),
		log:     log.With("network", n.Name),
	}
	// NickServ IDENTIFY is the pre-SASL fallback for networks that don't offer
	// the SASL capability. Only meaningful for real IRC (Twitch has no NickServ).
	if n.Kind != "twitch" && n.NickServPassEnv != "" {
		c.nickservPass = getenv(n.NickServPassEnv)
	}
	return c
}

// bind registers connect/message callbacks.
func (m *Manager) bind(c *conn) {
	ic := c.ic
	ic.AddConnectCallback(func(ircmsg.Message) {
		c.log.Info("connected", "nick", ic.CurrentNick())
		// Identify to services before joining, in case a channel is +r
		// (registered-only). NickServ processes this asynchronously; for
		// unrestricted channels the ordering doesn't matter.
		if c.nickservPass != "" {
			if err := ic.Privmsg("NickServ", "IDENTIFY "+c.nickservPass); err != nil {
				c.log.Warn("nickserv identify failed", "err", err)
			} else {
				c.log.Info("sent NickServ IDENTIFY")
			}
		}
		for _, ch := range c.cfg.Channels {
			if err := ic.Join(ch); err != nil {
				c.log.Warn("join failed", "channel", ch, "err", err)
			}
		}
	})
	ic.AddCallback("PRIVMSG", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		target := e.Params[0]
		private := !isChannel(target)
		channel := target
		if private {
			channel = e.Nick()
		}
		// Verified identity: Twitch login is stable, otherwise the services
		// account from the account-tag cap (empty if the user isn't logged in).
		account := ""
		if c.cfg.Kind == "twitch" {
			account = e.Nick()
		} else if ok, v := e.GetTag("account"); ok {
			account = v
		}
		ident, host := event.SplitUserHost(e.Source)
		m.handler(engine.Message{
			Network: c.cfg.Name,
			Channel: channel,
			Nick:    e.Nick(),
			Text:    e.Params[1],
			Private: private,
			Self:    ic.CurrentNick(),
			Account: account,
			Ident:   ident,
			Host:    host,
		})
	})

	// Presence events feed the dispatcher (tells, channel-keeping, idlerpg, …).
	ic.AddCallback("JOIN", func(e ircmsg.Message) {
		if len(e.Params) < 1 {
			return
		}
		ident, host := event.SplitUserHost(e.Source)
		m.emit(event.Event{
			Kind: event.Join, Network: c.cfg.Name, Channel: e.Params[0],
			Nick: e.Nick(), Ident: ident, Host: host, Account: joinAccount(e),
		})
		if c.keeper != nil {
			c.keeper.onJoin(e.Params[0], e.Nick())
		}
	})
	ic.AddCallback("PART", func(e ircmsg.Message) {
		if len(e.Params) < 1 {
			return
		}
		ident, host := event.SplitUserHost(e.Source)
		m.emit(event.Event{
			Kind: event.Part, Network: c.cfg.Name, Channel: e.Params[0],
			Nick: e.Nick(), Ident: ident, Host: host, Text: paramAt(e, 1),
		})
		if c.keeper != nil {
			c.keeper.onLeave(e.Params[0], e.Nick())
		}
	})
	ic.AddCallback("QUIT", func(e ircmsg.Message) {
		ident, host := event.SplitUserHost(e.Source)
		m.emit(event.Event{
			Kind: event.Quit, Network: c.cfg.Name,
			Nick: e.Nick(), Ident: ident, Host: host, Text: paramAt(e, 0),
		})
		if c.keeper != nil {
			c.keeper.onQuit(e.Nick())
		}
	})
	ic.AddCallback("NICK", func(e ircmsg.Message) {
		if len(e.Params) >= 1 {
			m.emit(event.Event{Kind: event.Nick, Network: c.cfg.Name, Nick: e.Nick(), Text: e.Params[0]})
		}
		if c.keeper != nil && len(e.Params) >= 1 {
			c.keeper.onNick(e.Nick(), e.Params[0])
		}
	})
	ic.AddCallback("KICK", func(e ircmsg.Message) {
		if len(e.Params) >= 2 {
			m.emit(event.Event{Kind: event.Kick, Network: c.cfg.Name, Channel: e.Params[0], Nick: e.Params[1], Actor: e.Nick()})
		}
		if c.keeper != nil && len(e.Params) >= 2 {
			c.keeper.onLeave(e.Params[0], e.Params[1])
		}
	})
	ic.AddCallback("MODE", func(e ircmsg.Message) {
		if len(e.Params) < 2 || !isChannel(e.Params[0]) {
			return
		}
		for _, ch := range opChanges(e.Params[1], e.Params[2:]) {
			m.emit(event.Event{
				Kind: event.Mode, Network: c.cfg.Name, Channel: e.Params[0],
				Nick: ch.nick, Actor: e.Nick(), Text: modeStr(ch.add) + "o",
			})
			if c.keeper != nil {
				c.keeper.onModeOp(e.Params[0], ch.nick, ch.add)
			}
		}
	})
	// RPL_NAMREPLY (353) and RPL_ENDOFNAMES (366) seed channel membership/op state.
	ic.AddCallback("353", func(e ircmsg.Message) {
		if len(e.Params) < 4 {
			return
		}
		if c.keeper != nil {
			c.keeper.onNames(e.Params[2], e.Params[3])
		}
		// Seed presence for already-present members (e.g. after a bot restart):
		// idlerpg marks enrolled idlers online without waiting for them to rejoin.
		channel := e.Params[2]
		for _, raw := range strings.Fields(e.Params[3]) {
			if nick := stripPrefixes(raw); nick != "" {
				m.emit(event.Event{Kind: event.Present, Network: c.cfg.Name, Channel: channel, Nick: nick})
			}
		}
	})
	ic.AddCallback("366", func(e ircmsg.Message) {
		if c.keeper != nil && len(e.Params) >= 2 {
			c.keeper.onEndNames(e.Params[1])
		}
	})
}

type opChange struct {
	nick string
	add  bool
}

func modeStr(add bool) string {
	if add {
		return "+"
	}
	return "-"
}

// opChanges parses a channel MODE change string + its args and returns the +o/-o
// changes. Modes that consume an argument are skipped over so 'o' args line up.
func opChanges(modes string, args []string) []opChange {
	const argModes = "ovhaqbeIkfjl" // modes that take a parameter
	var out []opChange
	add, ai := true, 0
	for i := 0; i < len(modes); i++ {
		switch modes[i] {
		case '+':
			add = true
		case '-':
			add = false
		default:
			var arg string
			if strings.IndexByte(argModes, modes[i]) >= 0 && ai < len(args) {
				arg = args[ai]
				ai++
			}
			if modes[i] == 'o' && arg != "" {
				out = append(out, opChange{nick: arg, add: add})
			}
		}
	}
	return out
}

// stripPrefixes removes leading membership-prefix sigils (@&~+%) from a NAMES
// entry, returning the bare nick.
func stripPrefixes(raw string) string {
	i := 0
	for i < len(raw) && strings.IndexByte("@&~+%", raw[i]) >= 0 {
		i++
	}
	return raw[i:]
}

// joinAccount extracts the joiner's services account: from the account-tag if
// present, else from the extended-join param (":nick JOIN #c account :real").
func joinAccount(e ircmsg.Message) string {
	if ok, a := e.GetTag("account"); ok && a != "" {
		return a
	}
	if len(e.Params) >= 2 && e.Params[1] != "*" {
		return e.Params[1]
	}
	return ""
}

// paramAt returns e.Params[i] or "" when out of range.
func paramAt(e ircmsg.Message, i int) string {
	if i < len(e.Params) {
		return e.Params[i]
	}
	return ""
}

// Run starts every connection plus its sender loop, returning immediately.
func (m *Manager) Run(ctx context.Context) {
	for _, name := range m.order {
		c := m.conns[name]
		m.wg.Add(2)
		go func(c *conn) { defer m.wg.Done(); c.runIRC(ctx) }(c)
		go func(c *conn) { defer m.wg.Done(); c.sendLoop(ctx) }(c)
	}
}

// runIRC connects and runs the event loop, retrying on failure until ctx ends.
func (c *conn) runIRC(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.ic.Connect(); err != nil {
			c.log.Error("connect failed, retrying", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
				continue
			}
		}
		c.ic.Loop() // blocks; handles its own reconnects until Quit()
		if ctx.Err() != nil {
			return
		}
		c.log.Warn("event loop exited, reconnecting")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// sendLoop drains the outbox, pacing sends through the rate limiter.
func (c *conn) sendLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.out:
			if d := c.limiter.Reserve(); d > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(d):
				}
			}
			var err error
			switch {
			case msg.notice:
				err = c.ic.Notice(msg.target, msg.text)
			case msg.action:
				err = c.ic.Action(msg.target, msg.text)
			default:
				err = c.ic.Privmsg(msg.target, msg.text)
			}
			if err != nil {
				c.log.Warn("send failed", "target", msg.target, "err", err)
			}
		}
	}
}

// Say queues a normal message (engine.Sender).
// Identify (re)sends a NickServ IDENTIFY on a network. With an explicit password
// it uses that; otherwise it falls back to the network's configured NickServ
// secret. Returns false for an unknown network or when no password is available
// (so the caller can tell the admin to pass one). The password is never logged.
func (m *Manager) Identify(network, password string) bool {
	c, ok := m.conns[network]
	if !ok {
		return false
	}
	if password == "" {
		password = c.nickservPass
	}
	if password == "" {
		return false
	}
	m.enqueue(network, outMsg{target: "NickServ", text: "IDENTIFY " + password})
	return true
}

func (m *Manager) Say(network, target, text string) {
	m.enqueue(network, outMsg{target: target, text: text})
}

// Notice queues an IRC NOTICE (engine.Noticer) — for automated, don't-auto-reply
// messages like the IdleRPG talk penalty.
func (m *Manager) Notice(network, target, text string) {
	m.enqueue(network, outMsg{target: target, text: text, notice: true})
}

// Action queues a CTCP ACTION / "/me" (engine.Sender).
func (m *Manager) Action(network, target, text string) {
	m.enqueue(network, outMsg{target: target, text: text, action: true})
}

func (m *Manager) enqueue(network string, o outMsg) {
	c, ok := m.conns[network]
	if !ok {
		m.log.Warn("send to unknown network", "network", network)
		return
	}
	select {
	case c.out <- o:
	default:
		c.log.Warn("outbox full, dropping message")
	}
}

// Quit asks every connection to disconnect without reconnecting.
func (m *Manager) Quit() {
	for _, c := range m.conns {
		c.ic.Quit()
	}
}

// Wait blocks until all goroutines have stopped.
func (m *Manager) Wait() { m.wg.Wait() }

// Networks returns the names of the networks this transport owns.
func (m *Manager) Networks() []string { return m.order }

// NetworkStatus reports each network's live connection state.
func (m *Manager) NetworkStatus() map[string]bool {
	out := make(map[string]bool, len(m.order))
	for _, name := range m.order {
		if c, ok := m.conns[name]; ok {
			out[name] = c.ic.Connected()
		}
	}
	return out
}

// Join makes the bot join a channel on the given network.
func (m *Manager) Join(network, channel string) {
	if c, ok := m.conns[network]; ok {
		if err := c.ic.Join(channel); err != nil {
			c.log.Warn("join failed", "channel", channel, "err", err)
		}
	}
}

// Part makes the bot leave a channel on the given network.
func (m *Manager) Part(network, channel string) {
	if c, ok := m.conns[network]; ok {
		if err := c.ic.Part(channel); err != nil {
			c.log.Warn("part failed", "channel", channel, "err", err)
		}
	}
}

// Invite sends an IRC INVITE for nick to channel. The bot must hold ops in the
// channel for this to take effect on invite-only channels.
func (m *Manager) Invite(network, nick, channel string) {
	if c, ok := m.conns[network]; ok {
		if err := c.ic.Send("INVITE", nick, channel); err != nil {
			c.log.Warn("invite failed", "nick", nick, "channel", channel, "err", err)
		}
	}
}

// Op grants channel-operator (+o) to nick in channel on the named network.
func (m *Manager) Op(network, channel, nick string) bool {
	return m.Mode(network, channel, "+o", nick)
}

// Mode applies a channel mode change to nick, but only if this bot currently
// holds ops there (tracked by the chankeeper). It returns true when it actually
// sent the mode. Requires op-state tracking — the chankeeper — otherwise it
// can't know it's opped and returns false.
func (m *Manager) Mode(network, channel, modes, nick string) bool {
	c, ok := m.conns[network]
	if !ok || c.keeper == nil || !c.keeper.HoldsOp(channel) {
		return false
	}
	if err := c.ic.Send("MODE", channel, modes, nick); err != nil {
		c.log.Warn("mode change failed", "channel", channel, "modes", modes, "nick", nick, "err", err)
		return false
	}
	c.log.Info("chanops: mode change", "channel", channel, "modes", modes, "nick", nick)
	return true
}

// Kick removes nick from the channel, but only if this bot holds ops there.
func (m *Manager) Kick(network, channel, nick, reason string) bool {
	c, ok := m.conns[network]
	if !ok || c.keeper == nil || !c.keeper.HoldsOp(channel) {
		return false
	}
	args := []string{channel, nick}
	if reason != "" {
		args = append(args, reason)
	}
	if err := c.ic.Send("KICK", args...); err != nil {
		c.log.Warn("kick failed", "channel", channel, "nick", nick, "err", err)
		return false
	}
	c.log.Info("chanops: kick", "channel", channel, "nick", nick, "reason", reason)
	return true
}

// AnyConnected reports whether at least one network is currently connected.
func (m *Manager) AnyConnected() bool {
	for _, c := range m.conns {
		if c.ic.Connected() {
			return true
		}
	}
	return false
}

// isChannel reports whether a PRIVMSG target is a channel rather than a nick.
func isChannel(target string) bool {
	if target == "" {
		return false
	}
	switch target[0] {
	case '#', '&', '+', '!':
		return true
	default:
		return false
	}
}

// hostOnly strips a trailing :port from a host:port string for TLS SNI.
func hostOnly(server string) string {
	if i := strings.LastIndex(server, ":"); i > 0 {
		return server[:i]
	}
	return server
}

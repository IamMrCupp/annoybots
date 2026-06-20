package admin

import (
	"context"
	"crypto/subtle"
	"sort"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/botnet"
	"github.com/IamMrCupp/annoybots/internal/engine"
)

// adminCommands is the set of recognized admin keywords. A message in a DM whose
// first token is one of these is treated as an admin command (and gated on auth).
var adminCommands = map[string]bool{
	"!admin": true, "!help": true,
	"!login": true, "!logout": true, "!claim": true,
	"!join": true, "!part": true, "!invite": true,
	"!say": true, "!act": true, "!identify": true,
	"!addquote": true, "!delquote": true,
	"!addadmin": true, "!deladmin": true, "!admins": true,
	"!reload": true,
	"!party":  true, "!unparty": true,
	"!networks": true,
}

// Handle processes a potential admin command. It only acts on DMs. Returns true
// if the message was an admin command (handled or rejected), so the caller knows
// to stop further processing.
func (m *Manager) Handle(ctx context.Context, msg engine.Message) bool {
	if !msg.Private {
		return false
	}
	text := strings.TrimSpace(msg.Text)
	fields := strings.Fields(text)
	if len(fields) == 0 || !adminCommands[strings.ToLower(fields[0])] {
		// A joined member's non-command DM is partyline chat — relay it.
		if len(fields) > 0 && m.isPartyMember(msg) {
			m.sendPartyline(msg, text)
			return true
		}
		return false
	}
	cmd := strings.ToLower(fields[0])

	// !login / !logout / !claim are how you authenticate or bootstrap, so they
	// bypass the admin gate (the claimer isn't an admin yet).
	switch cmd {
	case "!login":
		m.handleLogin(msg, text)
		return true
	case "!logout":
		m.clearSession(msg)
		m.reply(msg, "logged out.")
		return true
	case "!claim":
		m.handleClaim(msg, text)
		return true
	}

	if need := requiredFlag(cmd); !m.has(msg, need) {
		m.reply(msg, "you are not an admin for that. (need flag +"+string(need)+"; try !login <password>)")
		return true
	}
	m.exec(ctx, msg, cmd, fields, text)
	return true
}

// handleLogin authenticates via the fallback password and grants a session.
func (m *Manager) handleLogin(msg engine.Message, text string) {
	if m.password == "" {
		m.reply(msg, "password login is disabled.")
		return
	}
	if m.throttled(msg) {
		m.reply(msg, "too many failed attempts; wait a minute.")
		return
	}
	pass := tailAfter(text, 1)
	if pass != "" && subtle.ConstantTimeCompare([]byte(pass), []byte(m.password)) == 1 {
		m.grantSession(msg)
		m.reply(msg, "authenticated for "+m.ttl.String()+". (nick-based session; weaker than services auth)")
		return
	}
	m.recordFail(msg)
	m.reply(msg, "nope.")
}

// handleClaim consumes the one-time bootstrap code: the first sender to present
// it (with a verified identity) becomes the owner. The code is burned on success.
func (m *Manager) handleClaim(msg engine.Message, text string) {
	m.mu.Lock()
	code := m.claimCode
	m.mu.Unlock()
	if code == "" {
		m.reply(msg, "claiming isn't available.")
		return
	}
	if m.throttled(msg) {
		m.reply(msg, "too many failed attempts; wait a minute.")
		return
	}
	got := tailAfter(text, 1)
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(code)) != 1 {
		m.recordFail(msg)
		m.reply(msg, "nope.")
		return
	}
	// Correct code — but we can only enshrine a VERIFIED identity as owner, so a
	// services-less nick can't claim (it would be spoofable). The code stays valid.
	if msg.Account == "" {
		m.reply(msg, "right code, but I can't verify your identity here. claim from Discord or a network with services (SASL/NickServ), or set an admin in the config.")
		return
	}
	id := Identity{Network: msg.Network, Account: msg.Account, Flags: string(flagOwner)}
	m.applyAdminAdd(id) // persists, burns the claim code, and rebuilds the auth set
	m.publish(botnet.Event{Type: botnet.EventAdminAdd, AdminNet: id.Network, Account: id.Account, Flags: id.Flags})
	m.log.Warn("admin claimed", "network", msg.Network, "account", msg.Account)
	m.reply(msg, "done — you're the owner now ("+msg.Account+"@"+msg.Network+"). the claim code is spent.")
}

func (m *Manager) reply(msg engine.Message, text string) {
	m.ctl.Say(msg.Network, msg.Channel, text)
}

func (m *Manager) exec(_ context.Context, msg engine.Message, cmd string, fields []string, text string) {
	switch cmd {
	case "!admin", "!help":
		m.reply(msg, "commands: !login <password> | !logout | "+
			"!join <net> <#chan> | !part <net> <#chan> | "+
			"!invite <net> <#chan> <nick> | !say <net> <target> <text> | "+
			"!act <net> <target> <text> | !identify <net> [password] | "+
			"!addquote <pack> <text> | "+
			"!delquote <pack> <text> | !addadmin <net|*> <account> | "+
			"!deladmin <net|*> <account> | !admins | !reload | "+
			"!party [text] | !unparty")

	case "!party":
		body := tailAfter(text, 1)
		if !m.joinParty(msg) {
			m.reply(msg, "you're on the partyline. just type to chat; !unparty to leave.")
			m.announceParty(msg, msg.Nick+" joined the partyline")
		}
		m.sendPartyline(msg, body) // no-op if body is empty

	case "!unparty":
		if m.leaveParty(msg) {
			m.reply(msg, "left the partyline.")
			m.announceParty(msg, msg.Nick+" left the partyline")
		} else {
			m.reply(msg, "you're not on the partyline.")
		}

	case "!join":
		if len(fields) < 3 {
			m.reply(msg, "usage: !join <network> <#channel>")
			return
		}
		m.ctl.Join(fields[1], fields[2])
		m.reply(msg, "joining "+fields[2]+" on "+fields[1])

	case "!part":
		if len(fields) < 3 {
			m.reply(msg, "usage: !part <network> <#channel>")
			return
		}
		m.ctl.Part(fields[1], fields[2])
		m.reply(msg, "parting "+fields[2]+" on "+fields[1])

	case "!invite":
		if len(fields) < 4 {
			m.reply(msg, "usage: !invite <network> <#channel> <nick>")
			return
		}
		m.ctl.Invite(fields[1], fields[3], fields[2])
		m.reply(msg, "inviting "+fields[3]+" to "+fields[2]+" on "+fields[1])

	case "!say", "!act":
		if len(fields) < 4 {
			m.reply(msg, "usage: "+cmd+" <network> <target> <text>")
			return
		}
		body := tailAfter(text, 3)
		if cmd == "!act" {
			m.ctl.Action(fields[1], fields[2], body)
		} else {
			m.ctl.Say(fields[1], fields[2], body)
		}
		m.reply(msg, "sent.")

	case "!identify":
		// Re-authenticate the bot to NickServ. With no password it uses the
		// network's configured secret (nothing sensitive typed in chat); pass one
		// explicitly only when the network has none configured.
		if len(fields) < 2 {
			m.reply(msg, "usage: !identify <network> [password]  (omit password to use the configured one)")
			return
		}
		network := fields[1]
		password := ""
		if len(fields) >= 3 {
			password = fields[2]
		}
		if m.ctl.Identify(network, password) {
			m.reply(msg, "sent NickServ IDENTIFY on "+network+".")
		} else {
			m.reply(msg, "couldn't identify on "+network+" — unknown network, or no password configured (try !identify "+network+" <password>).")
		}

	case "!addquote":
		if len(fields) < 3 {
			m.reply(msg, "usage: !addquote <pack> <text>")
			return
		}
		pack, line := fields[1], tailAfter(text, 2)
		if m.applyQuoteAdd(pack, line) {
			m.publish(botnet.Event{Type: botnet.EventQuoteAdd, Pack: pack, Line: line})
			m.reply(msg, "added to "+pack+".")
		} else {
			m.reply(msg, "already present (or empty).")
		}

	case "!delquote":
		if len(fields) < 3 {
			m.reply(msg, "usage: !delquote <pack> <text>")
			return
		}
		pack, line := fields[1], tailAfter(text, 2)
		if m.applyQuoteDel(pack, line) {
			m.publish(botnet.Event{Type: botnet.EventQuoteDel, Pack: pack, Line: line})
			m.reply(msg, "removed from "+pack+".")
		} else {
			m.reply(msg, "not a runtime-added line (file quotes can't be removed).")
		}

	case "!addadmin":
		if len(fields) < 3 {
			m.reply(msg, "usage: !addadmin <network|*> <account> [flags] (n/m/o/v/f; default o)")
			return
		}
		flags := normalizeFlags(strings.Join(fields[3:], ""), string(flagOp))
		id := Identity{Network: normNet(fields[1]), Account: fields[2], Flags: flags}
		if m.applyAdminAdd(id) {
			m.publish(botnet.Event{Type: botnet.EventAdminAdd, AdminNet: id.Network, Account: id.Account, Flags: flags})
			m.reply(msg, "added "+id.Account+"@"+netOrAny(id.Network)+" (+"+flags+")")
		} else {
			m.reply(msg, "already an admin.")
		}

	case "!deladmin":
		if len(fields) < 3 {
			m.reply(msg, "usage: !deladmin <network|*> <account>")
			return
		}
		id := Identity{Network: normNet(fields[1]), Account: fields[2]}
		if m.applyAdminDel(id) {
			m.publish(botnet.Event{Type: botnet.EventAdminDel, AdminNet: id.Network, Account: id.Account})
			m.reply(msg, "removed admin "+id.Account)
		} else {
			m.reply(msg, "not removable (unknown, or defined in the config file).")
		}

	case "!admins":
		m.reply(msg, "admins: "+m.adminList())

	case "!networks":
		st := m.ctl.NetworkStatus()
		if len(st) == 0 {
			m.reply(msg, "no networks configured.")
			return
		}
		names := make([]string, 0, len(st))
		for n := range st {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, n := range names {
			status := "offline"
			if st[n] {
				status = "connected"
			}
			parts = append(parts, n+" ("+status+")")
		}
		m.reply(msg, "networks: "+strings.Join(parts, ", "))

	case "!reload":
		if m.reload == nil {
			m.reply(msg, "reload is not available.")
			return
		}
		summary, err := m.reload()
		if err != nil {
			m.reply(msg, "reload failed: "+err.Error())
			return
		}
		m.reply(msg, "reloaded "+summary+" (note: network/personality changes still need a restart)")
	}
}

// --- apply functions: shared by local commands and inbound bus events ---

func (m *Manager) applyQuoteAdd(pack, line string) bool {
	if !m.eng.AddQuote(pack, line) {
		return false
	}
	m.mu.Lock()
	m.quotes[pack] = append(m.quotes[pack], strings.TrimSpace(line))
	m.save()
	m.mu.Unlock()
	return true
}

func (m *Manager) applyQuoteDel(pack, line string) bool {
	removed := m.eng.DelQuote(pack, line)
	line = strings.TrimSpace(line)
	m.mu.Lock()
	lines := m.quotes[pack]
	for i, l := range lines {
		if l == line {
			m.quotes[pack] = append(lines[:i:i], lines[i+1:]...)
			break
		}
	}
	m.save()
	m.mu.Unlock()
	return removed
}

func (m *Manager) applyAdminAdd(id Identity) bool {
	if id.Account == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.admins[key(id.Network, id.Account)]; ok {
		return false
	}
	m.runtime = append(m.runtime, id)
	m.claimCode = "" // any successful admin-add closes the bootstrap claim window (local or bus-synced)
	m.rebuild()
	m.save()
	return true
}

func (m *Manager) applyAdminDel(id Identity) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(id.Network, id.Account)
	if _, ok := m.configKeys[k]; ok {
		return false // config admins are not runtime-removable
	}
	removed := false
	out := m.runtime[:0]
	for _, a := range m.runtime {
		if key(a.Network, a.Account) == k {
			removed = true
			continue
		}
		out = append(out, a)
	}
	m.runtime = out
	if removed {
		m.rebuild()
		m.save()
	}
	return removed
}

func (m *Manager) adminList() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var parts []string
	for _, a := range m.cfg.Admins {
		parts = append(parts, a.Account+"@"+netOrAny(a.Network)+"(+"+normalizeFlags(a.Flags, string(flagOwner))+")")
	}
	for _, a := range m.runtime {
		parts = append(parts, a.Account+"@"+netOrAny(a.Network)+"(+"+normalizeFlags(a.Flags, string(flagOp))+")")
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

// tailAfter returns the text following the first n whitespace-delimited tokens,
// preserving the remainder's internal spacing.
func tailAfter(text string, n int) string {
	s := strings.TrimSpace(text)
	for i := 0; i < n; i++ {
		idx := strings.IndexAny(s, " \t")
		if idx < 0 {
			return ""
		}
		s = strings.TrimLeft(s[idx:], " \t")
	}
	return s
}

func normNet(s string) string {
	if s == "*" || strings.EqualFold(s, "any") {
		return ""
	}
	return s
}

func netOrAny(s string) string {
	if s == "" {
		return "*"
	}
	return s
}

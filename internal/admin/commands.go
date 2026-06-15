package admin

import (
	"context"
	"crypto/subtle"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/botnet"
	"github.com/IamMrCupp/annoybots/internal/engine"
)

// adminCommands is the set of recognized admin keywords. A message in a DM whose
// first token is one of these is treated as an admin command (and gated on auth).
var adminCommands = map[string]bool{
	"!admin": true, "!help": true,
	"!login": true, "!logout": true,
	"!join": true, "!part": true, "!invite": true,
	"!say": true, "!act": true,
	"!addquote": true, "!delquote": true,
	"!addadmin": true, "!deladmin": true, "!admins": true,
	"!reload": true,
	"!party":  true, "!unparty": true,
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

	// !login / !logout are how you authenticate, so they bypass the admin gate.
	switch cmd {
	case "!login":
		m.handleLogin(msg, text)
		return true
	case "!logout":
		m.clearSession(msg)
		m.reply(msg, "logged out.")
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

func (m *Manager) reply(msg engine.Message, text string) {
	m.ctl.Say(msg.Network, msg.Channel, text)
}

func (m *Manager) exec(_ context.Context, msg engine.Message, cmd string, fields []string, text string) {
	switch cmd {
	case "!admin", "!help":
		m.reply(msg, "commands: !login <password> | !logout | "+
			"!join <net> <#chan> | !part <net> <#chan> | "+
			"!invite <net> <#chan> <nick> | !say <net> <target> <text> | "+
			"!act <net> <target> <text> | !addquote <pack> <text> | "+
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

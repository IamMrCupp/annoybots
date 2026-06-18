// Package account links a person's per-network identities (IRC, Twitch, Discord)
// into one shared account, so cross-network features — IdleRPG, karma — can treat
// them as a single player. You !register an account from one network, then !link
// from the others with the password. State lives in the F3 store.
//
// Note: the admin console already owns !login (for admin sessions), so account
// linking uses !register / !link to avoid the collision.
package account

import (
	"context"
	"log/slog"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/state"
)

// Manager handles account commands and resolves identities to accounts.
type Manager struct {
	store state.Store
	out   engine.Sender
	log   *slog.Logger
}

// New returns a Manager backed by store, replying via out.
func New(store state.Store, out engine.Sender, log *slog.Logger) *Manager {
	return &Manager{store: store, out: out, log: log}
}

// identity is the stable per-network key for a sender: the verified account when
// present (Discord ID, Twitch login, IRC services account), else the nick.
func identity(network, account, nick string) string {
	id := account
	if id == "" {
		id = nick
	}
	return strings.ToLower(network) + "|" + strings.ToLower(id)
}

func linkKey(id string) string  { return "acct:id:" + id }
func pwKey(name string) string  { return "acct:pw:" + name }
func memKey(name string) string { return "acct:links:" + name }

// Resolve returns the canonical player key for a sender: their linked account
// name if any, else their raw network identity (so unlinked users stay per-network).
func (m *Manager) Resolve(network, account, nick string) string {
	id := identity(network, account, nick)
	if acct, err := m.store.GetStr(context.Background(), linkKey(id)); err == nil && acct != "" {
		return acct
	}
	return id
}

// Handle processes account commands (DM-only). Returns true if it consumed one.
func (m *Manager) Handle(msg engine.Message) bool {
	if !msg.Private {
		return false
	}
	fields := strings.Fields(msg.Text)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "!register":
		m.register(msg, fields)
	case "!link":
		m.link(msg, fields)
	case "!whoami":
		m.whoami(msg)
	case "!unlink":
		m.unlink(msg)
	default:
		return false
	}
	return true
}

func (m *Manager) register(msg engine.Message, fields []string) {
	if len(fields) < 3 {
		m.reply(msg, "usage: !register <name> <password>")
		return
	}
	name := normName(fields[1])
	if name == "" {
		m.reply(msg, "name must be letters/numbers/_- (1-32 chars).")
		return
	}
	ctx := context.Background()
	if h, _ := m.store.GetStr(ctx, pwKey(name)); h != "" {
		m.reply(msg, "that account name is taken.")
		return
	}
	id := identity(msg.Network, msg.Account, msg.Nick)
	if existing, _ := m.store.GetStr(ctx, linkKey(id)); existing != "" {
		m.reply(msg, "this identity is already linked to '"+existing+"'. !unlink first.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(fields[2]), bcrypt.DefaultCost)
	if err != nil {
		m.log.Warn("bcrypt failed", "err", err)
		m.reply(msg, "couldn't create the account, try again.")
		return
	}
	_ = m.store.SetStr(ctx, pwKey(name), string(hash))
	_ = m.store.SetStr(ctx, linkKey(id), name)
	_ = m.store.HSet(ctx, memKey(name), id, 1)
	m.reply(msg, "registered '"+name+"' and linked this identity. From your other networks, DM me: !link "+name+" <password>")
}

func (m *Manager) link(msg engine.Message, fields []string) {
	if len(fields) < 3 {
		m.reply(msg, "usage: !link <name> <password>")
		return
	}
	name := normName(fields[1])
	ctx := context.Background()
	hash, _ := m.store.GetStr(ctx, pwKey(name))
	if hash == "" {
		m.reply(msg, "no such account.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(fields[2])) != nil {
		m.reply(msg, "wrong password.")
		return
	}
	id := identity(msg.Network, msg.Account, msg.Nick)
	_ = m.store.SetStr(ctx, linkKey(id), name)
	_ = m.store.HSet(ctx, memKey(name), id, 1)
	m.reply(msg, "linked this identity to '"+name+"'. you're now one player across networks.")
}

func (m *Manager) whoami(msg engine.Message) {
	ctx := context.Background()
	id := identity(msg.Network, msg.Account, msg.Nick)
	name, _ := m.store.GetStr(ctx, linkKey(id))
	if name == "" {
		m.reply(msg, "this identity isn't linked. !register <name> <password> to start, or !link <name> <password>.")
		return
	}
	links, _ := m.store.HGetAll(ctx, memKey(name))
	var ids []string
	for k, v := range links {
		if v == 1 {
			ids = append(ids, k)
		}
	}
	m.reply(msg, "account '"+name+"' — linked: "+strings.Join(ids, ", "))
}

func (m *Manager) unlink(msg engine.Message) {
	ctx := context.Background()
	id := identity(msg.Network, msg.Account, msg.Nick)
	name, _ := m.store.GetStr(ctx, linkKey(id))
	if name == "" {
		m.reply(msg, "this identity isn't linked.")
		return
	}
	_ = m.store.Del(ctx, linkKey(id))
	_ = m.store.HSet(ctx, memKey(name), id, 0) // mark removed (HGetAll filters value==1)
	m.reply(msg, "unlinked this identity from '"+name+"'.")
}

func (m *Manager) reply(msg engine.Message, text string) {
	m.out.Say(msg.Network, msg.Channel, text)
}

// normName keeps account names safe: lowercase letters/digits/_/- only, 1-32.
func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || len(s) > 32 {
		return ""
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !ok {
			return ""
		}
	}
	return s
}

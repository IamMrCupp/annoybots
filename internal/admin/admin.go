// Package admin implements a chat-based admin console. Admins (identified by
// their verified per-platform account, never by spoofable nick) can DM a bot to
// control channels, puppet it, and manage quote packs and the admin list at
// runtime. Quote/admin changes persist to disk and sync to the sibling bots over
// the botnet bus; channel control and puppeting stay local to the DMed bot.
package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/mrcupp/annoybots/internal/botnet"
	"github.com/mrcupp/annoybots/internal/engine"
)

// Quoter is the slice of the engine the console manipulates.
type Quoter interface {
	AddQuote(pack, line string) bool
	DelQuote(pack, line string) bool
	PackNames() []string
}

// Control is the send + channel-control surface (satisfied by the Router).
type Control interface {
	engine.Sender
	Join(network, channel string)
	Part(network, channel string)
	Invite(network, nick, channel string)
}

// Identity is an admin's verified identity on a network. A blank Network matches
// that account on any network.
type Identity struct {
	Network string `yaml:"network" json:"network"`
	Account string `yaml:"account" json:"account"`
}

func key(network, account string) string {
	return strings.ToLower(network) + "|" + strings.ToLower(account)
}

// Config configures the admin console.
type Config struct {
	Enabled   bool       `yaml:"enabled"`
	StatePath string     `yaml:"state_path"`
	Admins    []Identity `yaml:"admins"`
}

// persisted is the on-disk state: runtime-added admins and quotes.
type persisted struct {
	Admins []Identity          `json:"admins"`
	Quotes map[string][]string `json:"quotes"`
}

// Manager is the per-bot admin console.
type Manager struct {
	bot string
	cfg Config
	eng Quoter
	ctl Control
	bus botnet.Bus
	log *slog.Logger

	mu         sync.Mutex
	configKeys map[string]bool     // admins defined in config (cannot be removed at runtime)
	runtime    []Identity          // admins added at runtime
	admins     map[string]bool     // combined auth set
	quotes     map[string][]string // runtime quotes, for persistence
}

// New builds the console, loading persisted state and applying persisted quotes
// to the engine. bus may be nil (admin still works locally, without sync).
func New(bot string, cfg Config, eng Quoter, ctl Control, bus botnet.Bus, log *slog.Logger) *Manager {
	m := &Manager{
		bot:        strings.ToLower(bot),
		cfg:        cfg,
		eng:        eng,
		ctl:        ctl,
		bus:        bus,
		log:        log,
		configKeys: make(map[string]bool),
		admins:     make(map[string]bool),
		quotes:     make(map[string][]string),
	}
	for _, a := range cfg.Admins {
		m.configKeys[key(a.Network, a.Account)] = true
	}
	m.load()
	m.rebuild()
	return m
}

// rebuild recomputes the combined auth set from config + runtime admins.
func (m *Manager) rebuild() {
	m.admins = make(map[string]bool)
	for k := range m.configKeys {
		m.admins[k] = true
	}
	for _, a := range m.runtime {
		m.admins[key(a.Network, a.Account)] = true
	}
}

// isAdmin reports whether a message's verified identity is an admin.
func (m *Manager) isAdmin(msg engine.Message) bool {
	if msg.Account == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.admins[key(msg.Network, msg.Account)] || m.admins[key("", msg.Account)]
}

func (m *Manager) load() {
	if m.cfg.StatePath == "" {
		return
	}
	data, err := os.ReadFile(m.cfg.StatePath)
	if err != nil {
		return // no state yet
	}
	var p persisted
	if err := json.Unmarshal(data, &p); err != nil {
		m.log.Warn("admin state parse failed", "err", err)
		return
	}
	m.runtime = p.Admins
	if p.Quotes != nil {
		m.quotes = p.Quotes
	}
	for pack, lines := range m.quotes {
		for _, ln := range lines {
			m.eng.AddQuote(pack, ln)
		}
	}
}

// save persists runtime admins + quotes. Caller must hold m.mu.
func (m *Manager) save() {
	if m.cfg.StatePath == "" {
		return
	}
	data, err := json.MarshalIndent(persisted{Admins: m.runtime, Quotes: m.quotes}, "", "  ")
	if err != nil {
		m.log.Warn("admin state marshal failed", "err", err)
		return
	}
	tmp := m.cfg.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		m.log.Warn("admin state write failed", "err", err)
		return
	}
	if err := os.Rename(tmp, m.cfg.StatePath); err != nil {
		m.log.Warn("admin state rename failed", "err", err)
	}
}

// Run subscribes to the bus and applies quote/admin changes published by sibling
// bots. It returns immediately; nil bus is a no-op.
func (m *Manager) Run(ctx context.Context) error {
	if m.bus == nil {
		return nil
	}
	ch, err := m.bus.Subscribe(ctx)
	if err != nil {
		return err
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				m.onBusEvent(e)
			}
		}
	}()
	return nil
}

// onBusEvent applies admin changes from OTHER bots (skipping our own echoes and
// non-admin event types).
func (m *Manager) onBusEvent(e botnet.Event) {
	if e.From == m.bot {
		return
	}
	switch e.Type {
	case botnet.EventQuoteAdd:
		m.applyQuoteAdd(e.Pack, e.Line)
	case botnet.EventQuoteDel:
		m.applyQuoteDel(e.Pack, e.Line)
	case botnet.EventAdminAdd:
		m.applyAdminAdd(Identity{Network: e.AdminNet, Account: e.Account})
	case botnet.EventAdminDel:
		m.applyAdminDel(Identity{Network: e.AdminNet, Account: e.Account})
	}
}

func (m *Manager) publish(e botnet.Event) {
	if m.bus == nil {
		return
	}
	e.From = m.bot
	_ = m.bus.Publish(context.Background(), e)
}

// Package admin implements a chat-based admin console. Admins (identified by
// their verified per-platform account, never by spoofable nick) can DM a bot to
// control channels, puppet it, and manage quote packs and the admin list at
// runtime. Quote/admin changes persist to disk and sync to the sibling bots over
// the botnet bus; channel control and puppeting stay local to the DMed bot.
package admin

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IamMrCupp/annoybots/internal/botnet"
	"github.com/IamMrCupp/annoybots/internal/engine"
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
	Identify(network, password string) bool
	NetworkStatus() map[string]bool
}

// Identity is an admin's verified identity on a network. A blank Network matches
// that account on any network.
type Identity struct {
	Network string `yaml:"network" json:"network"`
	Account string `yaml:"account" json:"account"`
	Mask    string `yaml:"mask" json:"mask"`   // hostmask glob (nick!ident@host); alternative to Account on services-less nets
	Flags   string `yaml:"flags" json:"flags"` // access flags (F2); empty = full (owner) for config admins
}

func key(network, account string) string {
	return strings.ToLower(network) + "|" + strings.ToLower(account)
}

// Config configures the admin console.
type Config struct {
	Enabled   bool       `yaml:"enabled"`
	StatePath string     `yaml:"state_path"`
	Admins    []Identity `yaml:"admins"`
	// Password fallback (for networks without services, or users not logged in).
	// The password itself is read from PasswordEnv, never stored in the file.
	PasswordEnv string          `yaml:"password_env"`
	SessionTTL  engine.Duration `yaml:"session_ttl"` // how long a !login lasts (default 30m)
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

	password string        // fallback admin password (empty disables !login)
	ttl      time.Duration // session lifetime
	now      func() time.Time
	reload   func() (string, error) // re-read quotes/skits from disk (set by main)

	mu          sync.Mutex
	configKeys  map[string]string    // config admin key -> flags (cannot be removed at runtime)
	configMasks []maskAdmin          // config hostmask admins (cannot be removed at runtime)
	runtime     []Identity           // admins added at runtime
	admins      map[string]string    // combined account auth set: key -> flags
	maskAdmins  []maskAdmin          // combined hostmask auth set
	quotes      map[string][]string  // runtime quotes, for persistence
	sessions    map[string]time.Time // network|nick -> session expiry (password logins)
	fails       map[string][]time.Time
	party       map[string]partyMember // network|nick -> joined partyline member
	bridge      *bridgeTarget          // if set, partyline traffic is echoed to this public channel
	claimCode   string                 // one-time bootstrap code; empty once any admin exists
}

// session/throttle tuning for the password fallback.
const (
	defaultSessionTTL = 30 * time.Minute
	maxFailedLogins   = 5
	failWindow        = time.Minute
)

// New builds the console, loading persisted state and applying persisted quotes
// to the engine. password is the fallback !login secret (empty disables it). bus
// may be nil (admin still works locally, without sync).
func New(bot string, cfg Config, password string, eng Quoter, ctl Control, bus botnet.Bus, log *slog.Logger) *Manager {
	ttl := cfg.SessionTTL.D()
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	m := &Manager{
		bot:        strings.ToLower(bot),
		cfg:        cfg,
		eng:        eng,
		ctl:        ctl,
		bus:        bus,
		log:        log,
		password:   password,
		ttl:        ttl,
		now:        time.Now,
		configKeys: make(map[string]string),
		admins:     make(map[string]string),
		quotes:     make(map[string][]string),
		sessions:   make(map[string]time.Time),
		fails:      make(map[string][]time.Time),
		party:      make(map[string]partyMember),
	}
	for _, a := range cfg.Admins {
		if a.Mask != "" {
			m.configMasks = append(m.configMasks, maskAdmin{
				network: a.Network, mask: a.Mask, flags: normalizeFlags(a.Flags, string(flagOwner)),
			})
			continue
		}
		m.configKeys[key(a.Network, a.Account)] = normalizeFlags(a.Flags, string(flagOwner))
	}
	m.load()
	m.rebuild()
	// Zero-touch bootstrap: an enabled console with no admins (config or persisted)
	// prints a one-time claim code. The first person to DM "!claim <code>" with a
	// verified identity becomes the owner — no password to invent or store. The
	// code lives only in memory, so a restart prints a fresh one until it's claimed.
	if m.cfg.Enabled && len(m.admins) == 0 && len(m.maskAdmins) == 0 {
		if code := generateClaimCode(); code != "" {
			m.claimCode = code
			m.log.Warn(`admin bootstrap: no admins configured — DM the bot "!claim `+code+`" to become owner`,
				"claim_code", code)
		}
	}
	return m
}

// generateClaimCode returns a short, single-use bootstrap code (40 bits of
// crypto-random, formatted XXXX-XXXX), or "" if randomness is unavailable.
func generateClaimCode() string {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	return s[:4] + "-" + s[4:8]
}

// SetReload registers the reload hook invoked by the !reload command. fn should
// re-read on-disk config (quote packs, skits) and apply it, returning a short
// human summary.
func (m *Manager) SetReload(fn func() (string, error)) { m.reload = fn }

// rebuild recomputes the combined auth set from config + runtime admins.
func (m *Manager) rebuild() {
	m.admins = make(map[string]string)
	for k, f := range m.configKeys {
		m.admins[k] = f
	}
	for _, a := range m.runtime {
		m.admins[key(a.Network, a.Account)] = normalizeFlags(a.Flags, string(flagOp))
	}
	// Hostmask admins come from config only (v1); runtime adds are account-based.
	m.maskAdmins = append([]maskAdmin(nil), m.configMasks...)
}

// isAdmin reports whether a message carries at least master access (verified
// identity or an active password-login session). Finer-grained gating uses has().
func (m *Manager) isAdmin(msg engine.Message) bool {
	return m.has(msg, flagMaster)
}

// IsAdmin is the exported authorization hook other features use to gate their own
// privileged commands (e.g. IdleRPG's !rpg pause). It reports whether the sender
// holds at least op access — works for in-channel messages too, since it matches
// the sender's verified identity, not the channel.
func (m *Manager) IsAdmin(msg engine.Message) bool {
	return m.has(msg, flagOp)
}

// grantSession records a password-login session for the sender.
func (m *Manager) grantSession(msg engine.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessKey(msg)] = m.now().Add(m.ttl)
}

// clearSession ends a sender's password-login session.
func (m *Manager) clearSession(msg engine.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessKey(msg))
}

// throttled reports whether the sender has too many recent failed logins, and
// records nothing (recordFail does that).
func (m *Manager) throttled(msg engine.Message) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(msg.Network, msg.Nick)
	cut := m.now().Add(-failWindow)
	kept := m.fails[k][:0]
	for _, t := range m.fails[k] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	m.fails[k] = kept
	return len(kept) >= maxFailedLogins
}

// recordFail notes a failed login attempt for throttling.
func (m *Manager) recordFail(msg engine.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(msg.Network, msg.Nick)
	m.fails[k] = append(m.fails[k], m.now())
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
	// Partyline relays even our own bot's events are handled inside onPartyline
	// (it drops self-echoes), so it must run before the general self-skip below.
	if e.Type == botnet.EventPartyline {
		m.onPartyline(e)
		return
	}
	if e.From == m.bot {
		return
	}
	switch e.Type {
	case botnet.EventQuoteAdd:
		m.applyQuoteAdd(e.Pack, e.Line)
	case botnet.EventQuoteDel:
		m.applyQuoteDel(e.Pack, e.Line)
	case botnet.EventAdminAdd:
		m.applyAdminAdd(Identity{Network: e.AdminNet, Account: e.Account, Flags: e.Flags})
	case botnet.EventAdminDel:
		m.applyAdminDel(Identity{Network: e.AdminNet, Account: e.Account})
	case botnet.EventJoinChan, botnet.EventPartChan:
		m.applyChannelControl(e.Type, e.Network, e.Channel)
	}
}

// applyChannelControl joins or parts a channel on this bot. It's the shared body
// of the local action and the one taken when a sibling broadcasts, so both paths
// behave identically.
func (m *Manager) applyChannelControl(evt, network, channel string) {
	if network == "" || channel == "" {
		return
	}
	switch evt {
	case botnet.EventJoinChan:
		m.ctl.Join(network, channel)
		m.log.Info("botnet channel control: joining", "network", network, "channel", channel)
	case botnet.EventPartChan:
		m.ctl.Part(network, channel)
		m.log.Info("botnet channel control: parting", "network", network, "channel", channel)
	}
}

func (m *Manager) publish(e botnet.Event) {
	if m.bus == nil {
		return
	}
	e.From = m.bot
	_ = m.bus.Publish(context.Background(), e)
}

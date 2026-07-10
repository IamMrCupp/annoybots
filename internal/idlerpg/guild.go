package idlerpg

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/state"
)

// Guilds are the durable social layer: heroes band together under a name, pool
// gold into a shared vault, and level a little faster whenever guildmates idle
// side by side. A guild's level is simply the sum of its members' levels, so a
// guild rises exactly as fast as the people in it.
//
// The whole roster lives in one JSON blob (like the quest and world boss), with
// an in-memory copy behind a mutex. Membership is a player key, so it follows a
// linked account across networks.

const (
	guildFoundCost = 500 // gold to found a guild — a real sink
	guildNameMax   = 24
	guildBonusPer  = 3  // % idle bonus per guildmate idling with you
	guildBonusCap  = 15 // ceiling on that bonus
	guildMaxSize   = 12
)

func guildsKey() string { return "rpg:guilds" }

// guild is one band of heroes. Members are player keys; Founder is one of them.
type guild struct {
	Name    string   `json:"name"`
	Founder string   `json:"founder"`
	Members []string `json:"members"`
	Vault   int64    `json:"vault"`
}

// guildBook is every guild in the realm, keyed by the guild's slug.
type guildBook struct {
	Guilds map[string]*guild `json:"guilds"`
}

// GuildView is the dashboard's read-only view of a guild.
type GuildView struct {
	Name    string
	Founder string
	Members int
	Level   int64 // summed levels of every member
	Vault   int64
}

// guildSlug is the lookup form of a guild name: lowercase, spaces collapsed.
func guildSlug(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func (m *Manager) loadGuilds(ctx context.Context) {
	blob, err := m.store.GetStr(ctx, guildsKey())
	if err != nil || blob == "" {
		return
	}
	var b guildBook
	if json.Unmarshal([]byte(blob), &b) != nil || b.Guilds == nil {
		return
	}
	m.gmu.Lock()
	m.guilds = &b
	m.gmu.Unlock()
}

// saveGuilds persists the book. The caller must not hold gmu.
func (m *Manager) saveGuilds(ctx context.Context) {
	m.gmu.Lock()
	blob, err := json.Marshal(m.guilds)
	m.gmu.Unlock()
	if err == nil {
		_ = m.store.SetStr(ctx, guildsKey(), string(blob))
	}
}

// book returns the in-memory guild book, creating it on first use. Callers must
// hold gmu.
func (m *Manager) book() *guildBook {
	if m.guilds == nil {
		m.guilds = &guildBook{Guilds: map[string]*guild{}}
	}
	if m.guilds.Guilds == nil {
		m.guilds.Guilds = map[string]*guild{}
	}
	return m.guilds
}

// guildOf returns the guild a player key belongs to, or nil.
func (m *Manager) guildOf(key string) *guild {
	m.gmu.Lock()
	defer m.gmu.Unlock()
	for _, g := range m.book().Guilds {
		for _, member := range g.Members {
			if member == key {
				return g
			}
		}
	}
	return nil
}

// guildPct is the idle-speed multiplier (percent) for a player, from how many of
// their guildmates are idling alongside them. Guildless or alone: no change.
func (m *Manager) guildPct(key string, roster []player) int64 {
	g := m.guildOf(key)
	if g == nil {
		return 100
	}
	together := 0
	for _, p := range roster {
		for _, member := range g.Members {
			if member == p.key {
				together++
				break
			}
		}
	}
	if together <= 1 {
		return 100
	}
	bonus := int64((together - 1) * guildBonusPer)
	if bonus > guildBonusCap {
		bonus = guildBonusCap
	}
	return 100 + bonus
}

// dropFromGuild removes a player from whatever guild they're in, disbanding it if
// they were the last one out. Used by leave and by character resets.
func (m *Manager) dropFromGuild(ctx context.Context, key string) {
	m.gmu.Lock()
	for slug, g := range m.book().Guilds {
		kept := g.Members[:0]
		for _, member := range g.Members {
			if member != key {
				kept = append(kept, member)
			}
		}
		g.Members = kept
		if len(g.Members) == 0 {
			delete(m.book().Guilds, slug)
		} else if g.Founder == key {
			g.Founder = g.Members[0] // the mantle passes
		}
	}
	m.gmu.Unlock()
	m.saveGuilds(ctx)
}

// ReadGuilds returns every guild, richest in levels first.
func ReadGuilds(ctx context.Context, store state.Store) ([]GuildView, error) {
	blob, err := store.GetStr(ctx, guildsKey())
	if err != nil || blob == "" {
		return nil, err
	}
	var b guildBook
	if json.Unmarshal([]byte(blob), &b) != nil {
		return nil, nil
	}
	out := make([]GuildView, 0, len(b.Guilds))
	for _, g := range b.Guilds {
		var level int64
		for _, member := range g.Members {
			sheet, _ := store.HGetAll(ctx, sheetKey(member))
			level += sheet["level"]
		}
		out = append(out, GuildView{
			Name: g.Name, Founder: displayName(g.Founder),
			Members: len(g.Members), Level: level, Vault: g.Vault,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level > out[j].Level
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// guildNameFor reads a player's guild name straight from the store — for the
// dashboard, which has no Manager.
func guildNameFor(ctx context.Context, store state.Store, key string) string {
	blob, err := store.GetStr(ctx, guildsKey())
	if err != nil || blob == "" {
		return ""
	}
	var b guildBook
	if json.Unmarshal([]byte(blob), &b) != nil {
		return ""
	}
	for _, g := range b.Guilds {
		for _, member := range g.Members {
			if member == key {
				return g.Name
			}
		}
	}
	return ""
}

// guildLevel sums the levels of the given members.
func (m *Manager) guildLevel(ctx context.Context, members []string) int64 {
	var total int64
	for _, member := range members {
		sheet, _ := m.store.HGetAll(ctx, sheetKey(member))
		total += sheet["level"]
	}
	return total
}

// guildBoard answers !rpg guilds — the guild leaderboard.
func (m *Manager) guildBoard() string {
	views, _ := ReadGuilds(context.Background(), m.store)
	if len(views) == 0 {
		return "no guilds yet. !rpg guild create <name> to found the first."
	}
	if len(views) > 5 {
		views = views[:5]
	}
	parts := make([]string, len(views))
	for i, v := range views {
		parts[i] = fmt.Sprintf("%d. %s (lvl %d, %d members)", i+1, v.Name, v.Level, v.Members)
	}
	return "🛡 guilds — " + strings.Join(parts, " · ")
}

// guildCmd handles the whole !rpg guild <verb> family.
func (m *Manager) guildCmd(msg engine.Message, fields []string) string {
	ctx := context.Background()
	key := m.resolve(msg.Network, msg.Account, msg.Nick)
	if s, _ := m.store.HGetAll(ctx, sheetKey(key)); len(s) == 0 {
		return "you're not playing. !rpg to start the grind."
	}
	if len(fields) < 3 {
		return m.guildStatus(ctx, msg.Nick, key)
	}
	switch strings.ToLower(fields[2]) {
	case "create", "found":
		guildless := m.guildOf(key) == nil
		out := m.guildCreate(ctx, msg.Nick, key, strings.Join(fields[3:], " "))
		if guildless && m.guildOf(key) != nil { // the founding took
			m.awardFeat(ctx, player{network: msg.Network, nick: msg.Nick, channel: msg.Channel, key: key}, 1<<9)
		}
		return out
	case "join":
		return m.guildJoin(ctx, msg.Nick, key, strings.Join(fields[3:], " "))
	case "leave":
		return m.guildLeave(ctx, msg.Nick, key)
	case "deposit", "donate":
		return m.guildDeposit(ctx, msg.Nick, key, fields)
	}
	return m.guildStatus(ctx, msg.Nick, key) // "!rpg guild <name>" reads as a lookup
}

func (m *Manager) guildStatus(ctx context.Context, nick, key string) string {
	g := m.guildOf(key)
	if g == nil {
		return nick + " belongs to no guild. !rpg guild create <name>, or join one."
	}
	m.gmu.Lock()
	names := make([]string, len(g.Members))
	members := append([]string(nil), g.Members...)
	for i, member := range g.Members {
		names[i] = displayName(member)
	}
	name, vault := g.Name, g.Vault
	m.gmu.Unlock()
	sort.Strings(names)
	return fmt.Sprintf("🛡 %s — level %d · vault %dg · %s",
		name, m.guildLevel(ctx, members), vault, strings.Join(names, ", "))
}

func (m *Manager) guildCreate(ctx context.Context, nick, key, name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "name your guild: !rpg guild create <name>"
	}
	if len(name) > guildNameMax {
		return fmt.Sprintf("that name is too long — %d characters at most.", guildNameMax)
	}
	if m.guildOf(key) != nil {
		return nick + " is already in a guild. !rpg guild leave first."
	}
	sheet, _ := m.store.HGetAll(ctx, sheetKey(key))
	if sheet["gold"] < guildFoundCost {
		return fmt.Sprintf("founding a guild costs %dg — %s has %dg.", guildFoundCost, nick, sheet["gold"])
	}
	slug := guildSlug(name)
	m.gmu.Lock()
	if _, taken := m.book().Guilds[slug]; taken {
		m.gmu.Unlock()
		return "a guild by that name already stands."
	}
	m.book().Guilds[slug] = &guild{Name: name, Founder: key, Members: []string{key}}
	m.gmu.Unlock()
	m.saveGuilds(ctx)
	_, _ = m.store.HIncr(ctx, sheetKey(key), "gold", -guildFoundCost)
	return fmt.Sprintf("🛡 %s founds %s for %dg. !rpg guild join %s to stand with them.", nick, name, guildFoundCost, name)
}

func (m *Manager) guildJoin(ctx context.Context, nick, key, name string) string {
	if name == "" {
		return "join which guild? !rpg guild join <name>"
	}
	if m.guildOf(key) != nil {
		return nick + " is already in a guild. !rpg guild leave first."
	}
	slug := guildSlug(name)
	m.gmu.Lock()
	g, ok := m.book().Guilds[slug]
	if !ok {
		m.gmu.Unlock()
		return "no guild by that name. !rpg guilds lists them."
	}
	if len(g.Members) >= guildMaxSize {
		m.gmu.Unlock()
		return fmt.Sprintf("%s is full (%d members).", g.Name, guildMaxSize)
	}
	g.Members = append(g.Members, key)
	gname := g.Name
	m.gmu.Unlock()
	m.saveGuilds(ctx)
	return fmt.Sprintf("🛡 %s joins %s. idle together and you both rise faster.", nick, gname)
}

func (m *Manager) guildLeave(ctx context.Context, nick, key string) string {
	g := m.guildOf(key)
	if g == nil {
		return nick + " belongs to no guild."
	}
	name := g.Name
	m.dropFromGuild(ctx, key)
	return fmt.Sprintf("🛡 %s leaves %s.", nick, name)
}

func (m *Manager) guildDeposit(ctx context.Context, nick, key string, fields []string) string {
	g := m.guildOf(key)
	if g == nil {
		return nick + " belongs to no guild."
	}
	if len(fields) < 4 {
		return "deposit how much? !rpg guild deposit <gold>"
	}
	amt, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || amt <= 0 {
		return "deposit a positive amount of gold."
	}
	sheet, _ := m.store.HGetAll(ctx, sheetKey(key))
	if sheet["gold"] < amt {
		return fmt.Sprintf("%s has only %dg.", nick, sheet["gold"])
	}
	_, _ = m.store.HIncr(ctx, sheetKey(key), "gold", -amt)
	m.gmu.Lock()
	g.Vault += amt
	name, vault := g.Name, g.Vault
	m.gmu.Unlock()
	m.saveGuilds(ctx)
	return fmt.Sprintf("🛡 %s gives %dg to %s — the vault holds %dg.", nick, amt, name, vault)
}

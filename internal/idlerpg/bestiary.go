package idlerpg

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
	"github.com/IamMrCupp/annoybots/internal/state"
)

// The bestiary is the completionist's thread: every species you slay is recorded,
// so "level up" stops being the only goal. A character can chase the last few
// unseen beasts for years. The collection log does the same for loot rarity.
//
// Two hashes per character — species → kills, rarity → finds — plus a realm-wide
// species tally the dashboard renders as a field guide.

func bestiaryKey(player string) string   { return "rpg:bt:" + player }
func realmBestiaryKey() string           { return "rpg:bt:realm" }
func collectionKey(player string) string { return "rpg:coll:" + player }

// speciesNames lists every nameable foe in the bestiary table, sorted. Dynamic
// foes (dungeon lords, world bosses) aren't species and never appear here.
func speciesNames() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(bestiary))
	for _, mon := range bestiary {
		if !seen[mon.Name] {
			seen[mon.Name] = true
			out = append(out, mon.Name)
		}
	}
	sort.Strings(out)
	return out
}

// isSpecies reports whether a name is a catalogued foe (not a generated one).
func isSpecies(name string) bool {
	for _, mon := range bestiary {
		if mon.Name == name {
			return true
		}
	}
	return false
}

// recordKill credits a slain species to the character and the realm.
func (m *Manager) recordKill(ctx context.Context, key, name string) {
	if !isSpecies(name) {
		return // a dungeon lord or world boss — not a catalogued species
	}
	_, _ = m.store.HIncr(ctx, bestiaryKey(key), name, 1)
	_, _ = m.store.HIncr(ctx, realmBestiaryKey(), name, 1)
}

// recordFind credits a found item's rarity to the character's collection log.
func (m *Manager) recordFind(ctx context.Context, key, rarity string) {
	_, _ = m.store.HIncr(ctx, collectionKey(key), rarity, 1)
}

// BestiaryEntry is one species and how often it's fallen.
type BestiaryEntry struct {
	Name  string
	Kills int64
	Boss  bool
}

// ReadBestiary returns the realm's field guide: every species, with how many have
// fallen realm-wide (0 for species nobody has met yet), bosses last.
func ReadBestiary(ctx context.Context, store state.Store) ([]BestiaryEntry, error) {
	counts, err := store.HGetAll(ctx, realmBestiaryKey())
	if err != nil {
		return nil, err
	}
	bossOf := map[string]bool{}
	for _, mon := range bestiary {
		bossOf[mon.Name] = mon.Boss
	}
	names := speciesNames()
	out := make([]BestiaryEntry, 0, len(names))
	for _, n := range names {
		out = append(out, BestiaryEntry{Name: n, Kills: counts[n], Boss: bossOf[n]})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Boss != out[j].Boss {
			return !out[i].Boss // ordinary foes first, legends last
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// charBestiary returns how many distinct species a character has slain, and the
// total number that exist.
func (m *Manager) charBestiary(ctx context.Context, key string) (seen, total int) {
	counts, _ := m.store.HGetAll(ctx, bestiaryKey(key))
	for _, n := range speciesNames() {
		if counts[n] > 0 {
			seen++
		}
	}
	return seen, len(speciesNames())
}

// BestiaryProgress reports a character's species progress for the dashboard.
func BestiaryProgress(ctx context.Context, store state.Store, key string) (seen, total int) {
	counts, _ := store.HGetAll(ctx, bestiaryKey(key))
	for _, n := range speciesNames() {
		if counts[n] > 0 {
			seen++
		}
	}
	return seen, len(speciesNames())
}

// CollectionOf returns a character's rarity find-counts, commonest tier first.
func CollectionOf(ctx context.Context, store state.Store, key string) []BestiaryEntry {
	counts, _ := store.HGetAll(ctx, collectionKey(key))
	out := make([]BestiaryEntry, 0, len(rarities))
	for _, r := range rarities {
		if n := counts[r.name]; n > 0 {
			out = append(out, BestiaryEntry{Name: r.name, Kills: n})
		}
	}
	return out
}

// bestiaryStatus answers !rpg bestiary — species progress, the favourite prey,
// and the rarest thing you've dug up.
func (m *Manager) bestiaryStatus(msg engine.Message, fields []string) string {
	ctx := context.Background()
	name := msg.Nick
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if len(fields) >= 3 {
		name = fields[2]
		pkey = m.resolve(msg.Network, "", name)
	}
	if s, _ := m.store.HGetAll(ctx, sheetKey(pkey)); len(s) == 0 {
		return name + " isn't playing. !rpg to start the grind."
	}
	seen, total := m.charBestiary(ctx, pkey)
	counts, _ := m.store.HGetAll(ctx, bestiaryKey(pkey))

	out := fmt.Sprintf("📖 %s's bestiary — %d/%d species", name, seen, total)
	if top, n := topCount(counts); top != "" {
		out += fmt.Sprintf(" · favourite prey: %s (%d)", top, n)
	}
	if coll := CollectionOf(ctx, m.store, pkey); len(coll) > 0 {
		parts := make([]string, len(coll))
		for i, c := range coll {
			parts[i] = fmt.Sprintf("%s %d", c.Name, c.Kills)
		}
		out += " · finds: " + strings.Join(parts, ", ")
	}
	if seen == total && total > 0 {
		out += " — the guide is complete!"
	}
	return out
}

// topCount returns the highest-counted key in a tally, ties broken by name.
func topCount(counts map[string]int64) (string, int64) {
	best, bestN := "", int64(0)
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if counts[n] > bestN {
			best, bestN = n, counts[n]
		}
	}
	return best, bestN
}

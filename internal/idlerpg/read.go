package idlerpg

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/state"
)

// This file exposes a read-only projection of the game state for out-of-process
// readers (the web dashboard). It deliberately lives in the idlerpg package so
// the Redis key schema (sheetKey/boardKey/classKey/itemField) stays in one place
// — a consumer never reconstructs raw keys.

// Ability is one D&D ability score and its modifier, for display.
type Ability struct {
	Name  string // "STR", "DEX", …
	Score int64
	Mod   int64
}

// ItemView is one equipped item, for display.
type ItemView struct {
	Slot   string
	Level  int64
	Rarity string // "common" … "legendary"
	Name   string // magic-item name, empty if unnamed
}

// CharView is a read-only snapshot of one character's sheet.
type CharView struct {
	Key        string     // canonical character key (network|nick, or a linked account)
	Name       string     // display name: the key with any "network|" prefix stripped
	Level      int64      // current level
	Rebirths   int64      // prestige count (times reborn)
	HP         int64      // current hit points
	MaxHP      int64      // hit-point ceiling
	Poisoned   bool       // carrying venom (damage-over-time)
	Blessed    bool       // under a temple blessing (combat buff)
	Gold       int64      // coin from monster kills
	Kills      int64      // monsters slain
	TTL        int64      // seconds to the next level
	Power      int64      // total equipment power (sum of item levels)
	Title      string     // earned honorific (e.g. "the Dragonslayer"), empty if none yet
	Align      string     // full 9-point alignment, e.g. "chaotic evil" / "true neutral"
	AlignClass string     // moral axis only ("good"/"neutral"/"evil"), for color styling
	Race       string     // chosen race, empty if unset
	Class      string     // class, empty if unset
	Pet        string     // companion's kind (e.g. "wolf"), empty if none
	Mount      string     // steed's name, empty if none
	Draughts   int64      // healing draughts carried
	DuelWins   int64      // career spar wins
	Location   string     // where on the map: at/travelling-to a town, or roaming
	Items      []ItemView // equipped items (only non-empty slots), in slot order
	Abilities  []Ability  // the six ability scores, in canonical order (empty if unrolled)
	Feats      []string   // earned one-time achievements, in display order
}

// QuestView is a read-only snapshot of the active quest.
type QuestView struct {
	Kind     string   // "time" or "map"
	Desc     string   // the objective flavor text
	Members  []string // party display nicks, sorted
	Deadline int64    // unix seconds the quest resolves ("time" quests)

	// Map quests: the party's position, the two waypoints, the current leg, and
	// the grid size — everything the dashboard needs to draw the journey.
	X, Y    int
	X1, Y1  int
	X2, Y2  int
	Stage   int
	MapSize int

	// Hunt quests: foes slain so far, and how many are needed.
	Target   int64
	Progress int64
}

// MapDot is a player's position on the world map.
type MapDot struct {
	Name  string
	X, Y  int
	Level int64
}

// Town is a named landmark on the world map.
type Town struct {
	Name    string
	X, Y    int
	Service string
	Terrain string
}

// WorldView is everything the dashboard needs to draw the world map: every placed
// player's position and the static towns.
type WorldView struct {
	Players []MapDot
	Towns   []Town
	Size    int
}

// ReadWorld returns the world map — up to limit placed players plus the towns.
func ReadWorld(ctx context.Context, store state.Store, limit int) (WorldView, error) {
	top, err := store.ZTop(ctx, boardKey(), limit)
	if err != nil {
		return WorldView{}, err
	}
	w := WorldView{Size: worldSize}
	for _, e := range towns {
		w.Towns = append(w.Towns, Town(e))
	}
	for _, e := range top {
		sheet, _ := store.HGetAll(ctx, sheetKey(e.Member))
		if sheet["mx"] == 0 || sheet["my"] == 0 {
			continue // not placed on the map yet
		}
		w.Players = append(w.Players, MapDot{
			Name: displayName(e.Member), X: int(sheet["mx"]), Y: int(sheet["my"]), Level: sheet["level"],
		})
	}
	return w, nil
}

// ReadLeaderboard returns up to n characters ranked by level (highest first).
func ReadLeaderboard(ctx context.Context, store state.Store, n int) ([]CharView, error) {
	top, err := store.ZTop(ctx, boardKey(), n)
	if err != nil {
		return nil, err
	}
	out := make([]CharView, 0, len(top))
	for _, e := range top {
		out = append(out, readChar(ctx, store, e.Member))
	}
	return out, nil
}

// ReadChar returns one character's view by its canonical key, and whether that
// character is enrolled (the dashboard links leaderboard rows to /p/<key>).
func ReadChar(ctx context.Context, store state.Store, key string) (CharView, bool) {
	sheet, err := store.HGetAll(ctx, sheetKey(key))
	if err != nil {
		return CharView{}, false
	}
	if _, ok := sheet["level"]; !ok {
		return CharView{}, false
	}
	return readChar(ctx, store, key), true
}

func readChar(ctx context.Context, store state.Store, key string) CharView {
	sheet, _ := store.HGetAll(ctx, sheetKey(key))
	class, _ := store.GetStr(ctx, classKey(key))
	race, _ := store.GetStr(ctx, raceKey(key))
	pet, _ := store.GetStr(ctx, petKey(key))
	mount, _ := store.GetStr(ctx, mountKey(key))
	var items []ItemView
	for _, s := range itemSlots {
		lvl := sheet[itemField(s)]
		if lvl <= 0 {
			continue
		}
		name, _ := store.GetStr(ctx, nameKey(key, s))
		items = append(items, ItemView{
			Slot: s, Level: lvl, Rarity: rarityName(sheet[rarityField(s)]), Name: name,
		})
	}
	var abil []Ability
	if sheet["str"] != 0 { // scores rolled
		for _, a := range abilityLabels {
			abil = append(abil, Ability{Name: a.label, Score: sheet[a.field], Mod: abilityMod(sheet[a.field])})
		}
	}
	return CharView{
		Key:        key,
		Name:       displayName(key),
		Level:      sheet["level"],
		Rebirths:   sheet["reb"],
		HP:         curHP(sheet, class),
		MaxHP:      maxHP(sheet, class),
		Poisoned:   poisoned(sheet),
		Blessed:    blessed(sheet),
		Gold:       sheet["gold"],
		Kills:      sheet["kills"],
		TTL:        sheet["ttl"],
		Power:      itemSum(sheet),
		Title:      titleFor(sheet),
		Align:      fullAlign(sheet["law"], sheet["align"]),
		AlignClass: alignName(sheet["align"]),
		Race:       race,
		Class:      class,
		Pet:        pet,
		Mount:      mount,
		Draughts:   sheet["pots"],
		DuelWins:   sheet["duelw"],
		Location:   mapLocation(sheet),
		Items:      items,
		Abilities:  abil,
		Feats:      featList(sheet["feats"]),
	}
}

// mapLocation describes where a character is on the world map, for display.
func mapLocation(sheet map[string]int64) string {
	x, y, placed := playerPos(sheet)
	if !placed {
		return "not on the map yet"
	}
	if dest := sheet["dest"]; dest > 0 && int(dest) <= len(towns) {
		return "travelling to " + towns[dest-1].Name
	}
	if t := atTown(x, y); t != nil {
		return "at " + t.Name + " (" + t.Service + ")"
	}
	nt, _ := nearestTown(x, y)
	return "roaming, near " + nt.Name
}

// displayName strips a leading "network|" from a character key for presentation.
func displayName(key string) string {
	if i := strings.IndexByte(key, '|'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// ReadQuest returns the active quest, or nil when none is running.
func ReadQuest(ctx context.Context, store state.Store) (*QuestView, error) {
	blob, err := store.GetStr(ctx, questKey())
	if err != nil {
		return nil, err
	}
	if blob == "" {
		return nil, nil
	}
	var q quest
	if json.Unmarshal([]byte(blob), &q) != nil {
		return nil, nil
	}
	members := questNicks(&q)
	sort.Strings(members)
	return &QuestView{
		Kind: q.Kind, Desc: q.Desc, Members: members, Deadline: q.Deadline,
		X: q.X, Y: q.Y, X1: q.X1, Y1: q.Y1, X2: q.X2, Y2: q.Y2,
		Stage: q.Stage, MapSize: mapSize,
		Target: q.Target, Progress: q.Progress,
	}, nil
}

// ReadFeed returns the most recent activity-feed events, newest first.
func ReadFeed(ctx context.Context, store state.Store, n int) ([]FeedEvent, error) {
	raw, err := store.ListRange(ctx, feedKey, n)
	if err != nil {
		return nil, err
	}
	out := make([]FeedEvent, 0, len(raw))
	for _, s := range raw {
		var e FeedEvent
		if json.Unmarshal([]byte(s), &e) == nil && e.Text != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// ReadCharFeed returns up to n recent feed events that mention a character by its
// display name — that hero's own slice of the realm's activity, newest first.
func ReadCharFeed(ctx context.Context, store state.Store, name string, n int) ([]FeedEvent, error) {
	all, err := ReadFeed(ctx, store, feedCap)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(name)
	out := make([]FeedEvent, 0, n)
	for _, e := range all {
		if strings.Contains(strings.ToLower(e.Text), needle) {
			out = append(out, e)
			if len(out) >= n {
				break
			}
		}
	}
	return out, nil
}

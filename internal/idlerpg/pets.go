package idlerpg

import (
	"context"
	"fmt"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Companions are loyal beasts that fight at your side. You earn one rarely — when
// you slay a boss and don't already have a companion — and it grants a small
// passive bonus in monster encounters (attack and/or damage). Stored as a single
// string key (the pet's kind); everything else is looked up from petKinds.

const bossPetChance = 2 // 1-in-N chance a boss kill yields a companion (if you have none)

type petDef struct {
	Name  string // canonical kind, e.g. "wolf"
	Blurb string // flavor for status lines
	Atk   int64  // bonus to your attack rolls in monster fights
	Dmg   int64  // bonus to your damage in monster fights
}

// petKinds is the roster a tamed companion is drawn from.
var petKinds = []petDef{
	{"wolf", "a loyal grey hunter", 1, 1},
	{"dire boar", "a tusked bruiser", 0, 2},
	{"hawk", "a keen-eyed raptor", 2, 0},
	{"imp", "a cackling familiar", 1, 1},
	{"owlbear", "a savage curiosity", 1, 2},
}

func petKey(player string) string { return "rpg:pet:" + player }

func petByName(name string) (petDef, bool) {
	for _, p := range petKinds {
		if p.Name == name {
			return p, true
		}
	}
	return petDef{}, false
}

// petOf returns a character's companion, if any.
func (m *Manager) petOf(ctx context.Context, key string) (petDef, bool) {
	name, _ := m.store.GetStr(ctx, petKey(key))
	if name == "" {
		return petDef{}, false
	}
	return petByName(name)
}

// maybeTamePet gives a companion to a boss-slayer who has none, on a chance.
// Returns the tamed pet's name (for the announcement) or "".
func (m *Manager) maybeTamePet(ctx context.Context, key string) string {
	if _, has := m.petOf(ctx, key); has {
		return ""
	}
	if m.roll(bossPetChance) != 0 {
		return ""
	}
	pet := petKinds[m.roll(len(petKinds))]
	_ = m.store.SetStr(ctx, petKey(key), pet.Name)
	return pet.Name
}

// petStatus answers !rpg pet — the sender's companion or a named other's.
func (m *Manager) petStatus(msg engine.Message, fields []string) string {
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
	pet, ok := m.petOf(ctx, pkey)
	if !ok {
		return name + " has no companion yet. slay a boss and one may take to you."
	}
	return fmt.Sprintf("🐾 %s is accompanied by %s — %s (+%d atk, +%d dmg in the wild).",
		name, pet.Name, pet.Blurb, pet.Atk, pet.Dmg)
}

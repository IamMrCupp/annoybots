package idlerpg

import (
	"context"
	"testing"

	"github.com/IamMrCupp/annoybots/internal/state"
)

// foundGuild enrolls nick, funds them, and founds a guild they lead.
func foundGuild(t *testing.T, m *Manager, st state.Store, nick, name string, gold int64) {
	t.Helper()
	m.Handle(chanMsg(nick, "!rpg"))
	_ = st.HSet(context.Background(), sheetKey("net|"+nick), "gold", gold)
	m.guildCmd(chanMsg(nick, "!rpg guild create "+name), []string{"!rpg", "guild", "create", name})
}

func TestGuildRaidNeedsVaultGold(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Poor", guildFoundCost)

	got := m.callRaid(ctx, "net", "#chan", "alice", "net|alice")
	if !contains(got, "costs") {
		t.Fatalf("an empty vault can't call a raid, got %q", got)
	}
	if g := m.guildOf("net|alice"); g == nil || g.Raid != nil {
		t.Fatal("no raid should have started")
	}
}

func TestGuildRaidSpendsVaultAndStarts(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Rich", guildFoundCost+guildRaidCost)
	m.guildCmd(chanMsg("alice", "!rpg guild deposit 1000"), []string{"!rpg", "guild", "deposit", "1000"})

	if got := m.callRaid(ctx, "net", "#chan", "alice", "net|alice"); got != "" {
		t.Fatalf("a successful raid call narrates in-channel, not by return value; got %q", got)
	}
	g := m.guildOf("net|alice")
	if g == nil || g.Raid == nil {
		t.Fatal("the raid should be underway")
	}
	if g.Vault != 0 {
		t.Fatalf("the raid should have drained the vault, %dg left", g.Vault)
	}
	if !r.has("GUILD RAID") {
		t.Fatalf("the raid should be announced, got %q", r.last())
	}
	// A second call while one runs is refused.
	if got := m.callRaid(ctx, "net", "#chan", "alice", "net|alice"); !contains(got, "already fighting") {
		t.Fatalf("only one raid at a time, got %q", got)
	}
}

func TestGuildRaidOnlyGuildmatesDamageIt(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Clan", guildFoundCost+guildRaidCost)
	m.guildCmd(chanMsg("alice", "!rpg guild deposit 1000"), []string{"!rpg", "guild", "deposit", "1000"})
	// bob is online but unaffiliated.
	m.Handle(chanMsg("bob", "!rpg"))
	_ = st.HSet(ctx, sheetKey("net|bob"), "level", 50)

	m.callRaid(ctx, "net", "#chan", "alice", "net|alice")
	m.guildRaidTick(ctx)

	g := m.guildOf("net|alice")
	if g == nil || g.Raid == nil {
		t.Fatal("the raid should still be running")
	}
	if _, hit := g.Raid.Damage["net|bob"]; hit {
		t.Fatal("a non-member must not damage a guild raid")
	}
	if g.Raid.Damage["net|alice"] <= 0 {
		t.Fatal("the guild's own member should have landed damage")
	}
}

func TestGuildRaidDepartsAtTheDeadline(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Slow", guildFoundCost+guildRaidCost)
	m.guildCmd(chanMsg("alice", "!rpg guild deposit 1000"), []string{"!rpg", "guild", "deposit", "1000"})
	m.callRaid(ctx, "net", "#chan", "alice", "net|alice")

	g := m.guildOf("net|alice")
	g.Raid.Deadline = m.now().Unix() - 1 // time's up
	m.guildRaidTick(ctx)

	if g.Raid != nil {
		t.Fatal("an expired raid should be cleared")
	}
	if !r.has("departs unbroken") {
		t.Fatalf("the departure should be announced, got %q", r.last())
	}
}

func TestGuildRaidVictorySplitsSpoils(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Victors", guildFoundCost+guildRaidCost)
	m.guildCmd(chanMsg("alice", "!rpg guild deposit 1000"), []string{"!rpg", "guild", "deposit", "1000"})
	m.callRaid(ctx, "net", "#chan", "alice", "net|alice")

	g := m.guildOf("net|alice")
	before, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	g.Raid.HP = 1 // one more blow finishes it
	m.guildRaidTick(ctx)

	if g.Raid != nil {
		t.Fatal("a slain champion should clear the raid")
	}
	after, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if after["gold"] <= before["gold"] {
		t.Fatalf("the victor should be richer: %d → %d", before["gold"], after["gold"])
	}
	if !r.has("falls to") {
		t.Fatalf("victory should be announced, got %q", r.last())
	}
	if g.Vault <= 0 {
		t.Fatal("the guild should keep a share of the hoard")
	}
}

func TestGuildPerksBuyAndApply(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Perky", guildFoundCost+5000)
	m.guildCmd(chanMsg("alice", "!rpg guild deposit 5000"), []string{"!rpg", "guild", "deposit", "5000"})

	// listing costs nothing and mentions every perk
	list := m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk"})
	for _, want := range []string{"swiftness", "fortune", "might"} {
		if !contains(list, want) {
			t.Fatalf("the perk list should mention %q, got %q", want, list)
		}
	}
	if m.guildSwiftness("net|alice") != 0 {
		t.Fatal("no perks bought yet")
	}
	got := m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "swiftness"})
	if !contains(got, "swiftness to 1") {
		t.Fatalf("buying should raise the perk, got %q", got)
	}
	if m.guildSwiftness("net|alice") != 2 {
		t.Fatalf("swiftness 1 should grant +2%%, got %d", m.guildSwiftness("net|alice"))
	}
	// fortune scales gold; might sharpens attacks
	m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "fortune"})
	if got := m.guildFortune("net|alice", 100); got != 105 {
		t.Fatalf("fortune 1 should give +5%%, got %d", got)
	}
	m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "might"})
	if m.guildMight("net|alice") != 1 {
		t.Fatalf("might 1 should grant +1 attack, got %d", m.guildMight("net|alice"))
	}
	// an unknown perk is rejected
	if got := m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "nonsense"}); !contains(got, "no such perk") {
		t.Fatalf("unknown perks should be rejected, got %q", got)
	}
}

func TestGuildPerksRespectTheVaultAndTheCap(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Broke", guildFoundCost)

	if got := m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "swiftness"}); !contains(got, "costs") {
		t.Fatalf("an empty vault can't buy perks, got %q", got)
	}
	// Fund heavily and max one perk out.
	g := m.guildOf("net|alice")
	g.Vault = 1_000_000
	def, _ := perkByName("might")
	for i := int64(0); i < def.max; i++ {
		m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "might"})
	}
	if got := m.guildMight("net|alice"); got != def.max {
		t.Fatalf("might should cap at %d, got %d", def.max, got)
	}
	if got := m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "might"}); !contains(got, "mastered") {
		t.Fatalf("a maxed perk should refuse further levels, got %q", got)
	}
}

func TestGuildPerksAndRaidSurviveAReload(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	foundGuild(t, m, st, "alice", "Persist", guildFoundCost+6000)
	m.guildCmd(chanMsg("alice", "!rpg guild deposit 6000"), []string{"!rpg", "guild", "deposit", "6000"})
	m.perkCmd(ctx, "alice", "net|alice", []string{"!rpg", "guild", "perk", "fortune"})
	m.callRaid(ctx, "net", "#chan", "alice", "net|alice")

	// A fresh Manager over the same store rehydrates both.
	m2, _, _ := newMgrOn(st)
	g := m2.guildOf("net|alice")
	if g == nil {
		t.Fatal("the guild should survive a restart")
	}
	if g.Perks["fortune"] != 1 {
		t.Fatalf("perks should persist, got %v", g.Perks)
	}
	if g.Raid == nil {
		t.Fatal("an in-flight raid should persist")
	}
}

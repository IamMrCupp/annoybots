package idlerpg

import (
	"context"
	"testing"
)

func TestGuildCreateJoinAndLeave(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", guildFoundCost)

	if got := m.guildCmd(chanMsg("alice", "!rpg guild"), []string{"!rpg", "guild"}); !contains(got, "no guild") {
		t.Fatalf("alice should start guildless, got %q", got)
	}
	got := m.guildCmd(chanMsg("alice", "!rpg guild create The Idle Hands"),
		[]string{"!rpg", "guild", "create", "The", "Idle", "Hands"})
	if !contains(got, "founds The Idle Hands") {
		t.Fatalf("alice should found the guild, got %q", got)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] != 0 {
		t.Fatalf("founding should cost %dg, gold left = %d", guildFoundCost, s["gold"])
	}
	// Case-insensitive lookup by name.
	got = m.guildCmd(chanMsg("bob", "!rpg guild join the idle hands"),
		[]string{"!rpg", "guild", "join", "the", "idle", "hands"})
	if !contains(got, "joins The Idle Hands") {
		t.Fatalf("bob should join, got %q", got)
	}
	if g := m.guildOf("net|bob"); g == nil || len(g.Members) != 2 {
		t.Fatal("the guild should hold both heroes")
	}
	// A second guild can't take the same name, and a member can't double-join.
	if got := m.guildCmd(chanMsg("bob", "!rpg guild join x"), []string{"!rpg", "guild", "join", "x"}); !contains(got, "already in a guild") {
		t.Fatalf("bob is already sworn, got %q", got)
	}
	if got := m.guildCmd(chanMsg("bob", "!rpg guild leave"), []string{"!rpg", "guild", "leave"}); !contains(got, "leaves") {
		t.Fatalf("bob should leave, got %q", got)
	}
	if m.guildOf("net|bob") != nil {
		t.Fatal("bob should be guildless after leaving")
	}
	if g := m.guildOf("net|alice"); g == nil || len(g.Members) != 1 {
		t.Fatal("the guild should survive with alice alone")
	}
}

func TestGuildFoundingNeedsGold(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", guildFoundCost-1)

	got := m.guildCmd(chanMsg("alice", "!rpg guild create Broke"), []string{"!rpg", "guild", "create", "Broke"})
	if !contains(got, "costs") {
		t.Fatalf("a pauper can't found a guild, got %q", got)
	}
	if m.guildOf("net|alice") != nil {
		t.Fatal("no guild should exist")
	}
}

func TestGuildDisbandsWhenTheLastMemberLeaves(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", guildFoundCost)
	m.guildCmd(chanMsg("alice", "!rpg guild create Solo"), []string{"!rpg", "guild", "create", "Solo"})

	m.guildCmd(chanMsg("alice", "!rpg guild leave"), []string{"!rpg", "guild", "leave"})
	views, _ := ReadGuilds(ctx, st)
	if len(views) != 0 {
		t.Fatalf("an empty guild should disband, got %v", views)
	}
}

func TestGuildVaultTakesDeposits(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", guildFoundCost+300)
	m.guildCmd(chanMsg("alice", "!rpg guild create Coffers"), []string{"!rpg", "guild", "create", "Coffers"})

	if got := m.guildCmd(chanMsg("alice", "!rpg guild deposit 900"), []string{"!rpg", "guild", "deposit", "900"}); !contains(got, "only") {
		t.Fatalf("can't deposit gold you don't have, got %q", got)
	}
	got := m.guildCmd(chanMsg("alice", "!rpg guild deposit 200"), []string{"!rpg", "guild", "deposit", "200"})
	if !contains(got, "vault holds 200g") {
		t.Fatalf("the vault should hold the deposit, got %q", got)
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] != 100 {
		t.Fatalf("the deposit should leave the purse, gold = %d", s["gold"])
	}
}

func TestGuildPctRewardsIdlingTogether(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", guildFoundCost)
	m.guildCmd(chanMsg("alice", "!rpg guild create Pals"), []string{"!rpg", "guild", "create", "Pals"})
	m.guildCmd(chanMsg("bob", "!rpg guild join Pals"), []string{"!rpg", "guild", "join", "Pals"})

	alice := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	bob := player{network: "net", nick: "bob", channel: "#chan", key: "net|bob"}

	if got := m.guildPct("net|alice", []player{alice}); got != 100 {
		t.Fatalf("idling alone earns no guild bonus, got %d", got)
	}
	if got := m.guildPct("net|alice", []player{alice, bob}); got != 100+guildBonusPer {
		t.Fatalf("one guildmate should add %d%%, got %d", guildBonusPer, got)
	}
	// A guildless hero standing nearby changes nothing.
	carol := player{network: "net", nick: "carol", channel: "#chan", key: "net|carol"}
	if got := m.guildPct("net|carol", []player{alice, bob, carol}); got != 100 {
		t.Fatalf("the guildless earn no guild bonus, got %d", got)
	}
	_ = ctx
}

func TestReadGuildsRanksBySummedLevels(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	m.Handle(chanMsg("bob", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", guildFoundCost)
	st.HSet(ctx, sheetKey("net|bob"), "gold", guildFoundCost)
	st.HSet(ctx, sheetKey("net|alice"), "level", 5)
	st.HSet(ctx, sheetKey("net|bob"), "level", 40)
	m.guildCmd(chanMsg("alice", "!rpg guild create Small"), []string{"!rpg", "guild", "create", "Small"})
	m.guildCmd(chanMsg("bob", "!rpg guild create Mighty"), []string{"!rpg", "guild", "create", "Mighty"})

	views, err := ReadGuilds(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].Name != "Mighty" || views[0].Level != 40 {
		t.Fatalf("guilds should rank by summed member level, got %+v", views)
	}
}

func TestFoundingAGuildEarnsGuildmaster(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "gold", guildFoundCost)

	m.guildCmd(chanMsg("alice", "!rpg guild create Founders"), []string{"!rpg", "guild", "create", "Founders"})
	s, _ := st.HGetAll(ctx, sheetKey("net|alice"))
	if s["feats"]&(1<<9) == 0 {
		t.Fatal("founding a guild should earn Guildmaster")
	}
	// Joining one shouldn't.
	m.Handle(chanMsg("bob", "!rpg"))
	m.guildCmd(chanMsg("bob", "!rpg guild join Founders"), []string{"!rpg", "guild", "join", "Founders"})
	s, _ = st.HGetAll(ctx, sheetKey("net|bob"))
	if s["feats"]&(1<<9) != 0 {
		t.Fatal("merely joining should not earn Guildmaster")
	}
}

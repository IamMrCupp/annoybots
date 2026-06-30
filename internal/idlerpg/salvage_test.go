package idlerpg

import (
	"context"
	"testing"
)

func TestSalvageGivesScrapGold(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	// give alice an existing weapon so the next find replaces (salvages) it
	st.HSet(ctx, sheetKey("net|alice"), itemField("weapon"), 5)
	st.HSet(ctx, sheetKey("net|alice"), rarityField("weapon"), 0)
	goldBefore := int64(0)
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] != goldBefore {
		goldBefore = s["gold"]
	}
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	// force a strong find by passing a high level (so it beats the level-5 weapon)
	for i := 0; i < 40; i++ {
		m.findItem(ctx, p, 60)
		if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] > goldBefore {
			break
		}
	}
	if s, _ := st.HGetAll(ctx, sheetKey("net|alice")); s["gold"] <= goldBefore {
		t.Fatalf("replacing gear should yield scrap gold, gold=%d", s["gold"])
	}
	if !r.has("salvaged the old one") {
		t.Fatalf("a salvage should be announced, got %v", r.lines)
	}
}

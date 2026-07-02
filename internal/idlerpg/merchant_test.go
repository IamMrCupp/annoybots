package idlerpg

import (
	"context"
	"testing"
)

func TestMerchantGifts(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	st.HSet(ctx, sheetKey("net|alice"), "level", 10)
	st.HSet(ctx, sheetKey("net|alice"), "ttl", 10000)
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}
	// run the merchant many times; each gift is positive (gold up, or pots up, or ttl down)
	for i := 0; i < 30; i++ {
		before, _ := st.HGetAll(ctx, sheetKey("net|alice"))
		m.merchant(ctx, p)
		after, _ := st.HGetAll(ctx, sheetKey("net|alice"))
		gained := after["gold"] > before["gold"] || after["pots"] > before["pots"] || after["ttl"] < before["ttl"]
		if !gained {
			t.Fatalf("merchant should always gift something positive: before=%v after=%v", before, after)
		}
	}
	if !r.has("wandering merchant") {
		t.Fatalf("merchant should announce, got %v", r.lines)
	}
}

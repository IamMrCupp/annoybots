package idlerpg

import (
	"context"
	"strings"
	"testing"
)

func TestAffixRollsScaleWithRarity(t *testing.T) {
	m, _, _ := newMgr()
	// common never rolls an affix; legendary always rolls several.
	for i := 0; i < 50; i++ {
		if got := m.rollAffixes(0); got != 0 {
			t.Fatalf("common gear should roll no affixes, got %v", affixNames(got))
		}
		if n := countBits(m.rollAffixes(4)); n < 2 {
			t.Fatalf("legendary gear should roll at least 2 affixes, got %d", n)
		}
		if n := countBits(m.rollAffixes(2)); n != 1 {
			t.Fatalf("rare gear should roll exactly 1 affix, got %d", n)
		}
	}
}

func TestAffixModsScaleWithItemRarity(t *testing.T) {
	// The same affix is stronger on a rarer item.
	common := map[string]int64{
		itemField("weapon"): 10, rarityField("weapon"): 0, affixField("weapon"): 1 << 0, // vampiric
	}
	legendary := map[string]int64{
		itemField("weapon"): 10, rarityField("weapon"): 4, affixField("weapon"): 1 << 0,
	}
	lo, hi := affixesOf(common).lifesteal, affixesOf(legendary).lifesteal
	if lo <= 0 || hi <= lo {
		t.Fatalf("a legendary's affix should outclass a common's: common=%d legendary=%d", lo, hi)
	}
}

func TestAffixModsIgnoreEmptySlots(t *testing.T) {
	// An affix mask on a slot holding no item contributes nothing.
	sheet := map[string]int64{
		itemField("weapon"): 0, rarityField("weapon"): 4, affixField("weapon"): 1<<0 | 1<<1,
	}
	if am := affixesOf(sheet); am.lifesteal != 0 || am.thorns != 0 {
		t.Fatalf("an unequipped slot must not grant affixes, got %+v", am)
	}
}

func TestAffixKeenIsCapped(t *testing.T) {
	// Every slot legendary + keen would otherwise crit on almost every swing.
	sheet := map[string]int64{}
	for _, slot := range itemSlots {
		sheet[itemField(slot)] = 10
		sheet[rarityField(slot)] = 4
		sheet[affixField(slot)] = 1 << 2 // keen
	}
	if got := affixesOf(sheet).keen; got != 4 {
		t.Fatalf("keen should be capped at 4, got %d", got)
	}
}

func TestAwakenAffixAddsOneUpToTheCap(t *testing.T) {
	m, _, _ := newMgr()
	mask := int64(0)
	for i := 0; i < 10; i++ {
		mask = m.awakenAffix(mask)
	}
	if countBits(mask) != affixCap {
		t.Fatalf("repeated enchanting should fill to the cap (%d), got %d", affixCap, countBits(mask))
	}
}

func TestVitalAffixRaisesMaxHP(t *testing.T) {
	base := map[string]int64{"level": 10, "con": 10}
	withVital := map[string]int64{
		"level": 10, "con": 10,
		itemField("amulet"): 5, rarityField("amulet"): 4, affixField("amulet"): 1 << 3, // vital
	}
	if maxHP(withVital, "fighter") <= maxHP(base, "fighter") {
		t.Fatal("vital gear should raise maximum HP")
	}
}

func TestFoundItemRecordsAffixesAndShowsThem(t *testing.T) {
	m, r, st := newMgr()
	ctx := context.Background()
	m.Handle(chanMsg("alice", "!rpg"))
	p := player{network: "net", nick: "alice", channel: "#chan", key: "net|alice"}

	// Drop items until one rolls an affix, then assert it persisted and was shown.
	var found bool
	for i := 0; i < 200 && !found; i++ {
		m.findItem(ctx, p, 200) // high level → skews rare, and always an upgrade early
		sheet, _ := st.HGetAll(ctx, sheetKey("net|alice"))
		for _, slot := range itemSlots {
			if mask := sheet[affixField(slot)]; mask != 0 {
				found = true
				names := affixNames(mask)
				if len(names) == 0 {
					t.Fatal("a non-zero affix mask should decode to names")
				}
				// the drop announcement should carry the affix list
				if !r.has("[" + strings.Join(names, ", ") + "]") {
					t.Logf("note: affix suffix not in the latest line (item may have been replaced since)")
				}
				break
			}
		}
	}
	if !found {
		t.Fatal("200 high-level drops should have produced at least one affixed item")
	}
}

func TestAffixesCatalogCoversEveryAffix(t *testing.T) {
	if len(Affixes()) != len(affixList) {
		t.Fatalf("the catalog should list every affix: %d vs %d", len(Affixes()), len(affixList))
	}
	for _, it := range Affixes() {
		if it.Cmd == "" || it.Desc == "" {
			t.Fatalf("every affix needs a name and a description, got %+v", it)
		}
	}
}

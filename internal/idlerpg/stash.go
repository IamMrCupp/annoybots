package idlerpg

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// The stash is a per-character inventory: bank an equipped item to free its slot,
// and equip a stashed one back later (swapping the current one in). It's a JSON
// list under a single key — the single game bot coordinates read-modify-write, so
// no cross-bot races. Coexists with salvage: auto-upgrades still scrap the old
// item for gold; the stash is for *deliberate* banking (e.g. holding a legendary
// while you experiment).

const stashCap = 12 // most items a character can bank

// stashItem is one banked piece of gear.
type stashItem struct {
	Slot   string `json:"slot"`
	Level  int64  `json:"lvl"`
	Rarity int64  `json:"rar"`
	Name   string `json:"name,omitempty"`
}

func stashKey(player string) string { return "rpg:stash:" + player }

func (m *Manager) readStash(ctx context.Context, key string) []stashItem {
	blob, _ := m.store.GetStr(ctx, stashKey(key))
	if blob == "" {
		return nil
	}
	var items []stashItem
	if json.Unmarshal([]byte(blob), &items) != nil {
		return nil
	}
	return items
}

func (m *Manager) writeStash(ctx context.Context, key string, items []stashItem) {
	if len(items) == 0 {
		_ = m.store.Del(ctx, stashKey(key))
		return
	}
	if blob, err := json.Marshal(items); err == nil {
		_ = m.store.SetStr(ctx, stashKey(key), string(blob))
	}
}

func stashLabel(it stashItem) string {
	s := fmt.Sprintf("%s %s lvl %d", rarityName(it.Rarity), it.Slot, it.Level)
	if it.Name != "" {
		s += " “" + it.Name + "”"
	}
	return s
}

// stash handles !rpg stash [slot] — list the stash, or bank an equipped item.
func (m *Manager) stash(msg engine.Message, fields []string) {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	items := m.readStash(ctx, pkey)

	if len(fields) < 3 { // list the stash
		if len(items) == 0 {
			m.out.Say(msg.Network, msg.Channel, msg.Nick+"'s stash is empty. !rpg stash <slot> to bank an equipped item.")
			return
		}
		parts := make([]string, len(items))
		for i, it := range items {
			parts[i] = fmt.Sprintf("%d) %s", i+1, stashLabel(it))
		}
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("🎒 %s's stash (%d/%d): %s — !rpg equip <#>",
			msg.Nick, len(items), stashCap, strings.Join(parts, " · ")))
		return
	}

	slot := strings.ToLower(fields[2])
	if !isItemSlot(slot) {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg stash <slot>. slots: "+strings.Join(itemSlots, ", "))
		return
	}
	if sheet[itemField(slot)] <= 0 {
		m.out.Say(msg.Network, msg.Channel, msg.Nick+" has no "+slot+" equipped to bank.")
		return
	}
	if len(items) >= stashCap {
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s's stash is full (%d). equip something first.", msg.Nick, stashCap))
		return
	}
	name, _ := m.store.GetStr(ctx, nameKey(pkey, slot))
	items = append(items, stashItem{Slot: slot, Level: sheet[itemField(slot)], Rarity: sheet[rarityField(slot)], Name: name})
	m.writeStash(ctx, pkey, items)
	_ = m.store.HSet(ctx, sheetKey(pkey), itemField(slot), 0)
	_ = m.store.HSet(ctx, sheetKey(pkey), rarityField(slot), 0)
	_ = m.store.Del(ctx, nameKey(pkey, slot))
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("🎒 %s banks their %s (%d/%d stashed).", msg.Nick, slot, len(items), stashCap))
}

// equip handles !rpg equip <#> — equip a stashed item, swapping the current one
// in that slot back into the stash.
func (m *Manager) equip(msg engine.Message, fields []string) {
	ctx := context.Background()
	pkey := m.resolve(msg.Network, msg.Account, msg.Nick)
	sheet, _ := m.store.HGetAll(ctx, sheetKey(pkey))
	if len(sheet) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	if len(fields) < 3 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg equip <#> — see !rpg stash for the numbers.")
		return
	}
	n, err := strconv.Atoi(fields[2])
	items := m.readStash(ctx, pkey)
	if err != nil || n < 1 || n > len(items) {
		m.out.Say(msg.Network, msg.Channel, "no such stash item. !rpg stash to see yours.")
		return
	}
	it := items[n-1]
	items = append(items[:n-1], items[n:]...) // remove the chosen item

	// swap the currently-equipped item in that slot back into the stash
	if sheet[itemField(it.Slot)] > 0 {
		curName, _ := m.store.GetStr(ctx, nameKey(pkey, it.Slot))
		items = append(items, stashItem{Slot: it.Slot, Level: sheet[itemField(it.Slot)], Rarity: sheet[rarityField(it.Slot)], Name: curName})
	}
	m.writeStash(ctx, pkey, items)

	_ = m.store.HSet(ctx, sheetKey(pkey), itemField(it.Slot), it.Level)
	_ = m.store.HSet(ctx, sheetKey(pkey), rarityField(it.Slot), it.Rarity)
	if it.Name != "" {
		_ = m.store.SetStr(ctx, nameKey(pkey, it.Slot), it.Name)
	} else {
		_ = m.store.Del(ctx, nameKey(pkey, it.Slot))
	}
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("🎒 %s equips the %s from the stash.", msg.Nick, stashLabel(it)))
}

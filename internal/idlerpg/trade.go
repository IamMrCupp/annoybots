package idlerpg

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Trading lets players share the wealth — hand gold or a stashed item to a friend.
// It's a straight transfer (gold is conserved), so nothing is minted or exploited;
// it just ties the realm's economy together now that people play alongside each
// other.

// give handles !rpg give <name> <amount> and !rpg give <name> item <#>.
func (m *Manager) give(msg engine.Message, fields []string) {
	if len(fields) < 4 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg give <name> <amount>, or !rpg give <name> item <#>")
		return
	}
	ctx := context.Background()
	fromKey := m.resolve(msg.Network, msg.Account, msg.Nick)
	if s, _ := m.store.HGetAll(ctx, sheetKey(fromKey)); len(s) == 0 {
		m.out.Say(msg.Network, msg.Channel, "you're not playing. !rpg to start the grind.")
		return
	}
	target := fields[2]
	if strings.EqualFold(target, msg.Nick) {
		m.out.Say(msg.Network, msg.Channel, "you can't give to yourself.")
		return
	}
	toKey := m.resolve(msg.Network, "", target)
	if s, _ := m.store.HGetAll(ctx, sheetKey(toKey)); len(s) == 0 {
		m.out.Say(msg.Network, msg.Channel, target+" isn't playing.")
		return
	}

	if strings.EqualFold(fields[3], "item") {
		m.giveItem(ctx, msg, fromKey, toKey, target, fields)
		return
	}

	amt, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || amt <= 0 {
		m.out.Say(msg.Network, msg.Channel, "the amount must be a positive number of gold.")
		return
	}
	from, _ := m.store.HGetAll(ctx, sheetKey(fromKey))
	if from["gold"] < amt {
		m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("%s only has %dg to give.", msg.Nick, from["gold"]))
		return
	}
	_, _ = m.store.HIncr(ctx, sheetKey(fromKey), "gold", -amt)
	_, _ = m.store.HIncr(ctx, sheetKey(toKey), "gold", amt)
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("💸 %s gives %dg to %s.", msg.Nick, amt, target))
}

// giveItem transfers a stashed item (by the giver's stash number) to the target's stash.
func (m *Manager) giveItem(ctx context.Context, msg engine.Message, fromKey, toKey, target string, fields []string) {
	if len(fields) < 5 {
		m.out.Say(msg.Network, msg.Channel, "usage: !rpg give <name> item <#> (the number from !rpg stash)")
		return
	}
	n, err := strconv.Atoi(fields[4])
	mine := m.readStash(ctx, fromKey)
	if err != nil || n < 1 || n > len(mine) {
		m.out.Say(msg.Network, msg.Channel, "no such stash item. !rpg stash to see yours.")
		return
	}
	theirs := m.readStash(ctx, toKey)
	if len(theirs) >= stashCap {
		m.out.Say(msg.Network, msg.Channel, target+"'s stash is full — they can't accept it.")
		return
	}
	it := mine[n-1]
	mine = append(mine[:n-1], mine[n:]...)
	theirs = append(theirs, it)
	m.writeStash(ctx, fromKey, mine)
	m.writeStash(ctx, toKey, theirs)
	m.out.Say(msg.Network, msg.Channel, fmt.Sprintf("🎁 %s gives %s a %s.", msg.Nick, target, stashLabel(it)))
}

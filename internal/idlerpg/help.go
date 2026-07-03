package idlerpg

import (
	"strings"

	"github.com/IamMrCupp/annoybots/internal/engine"
)

// Command help is defined once here and rendered two ways — the terse in-channel
// !rpg help, and the full grouped reference on the web dashboard's /help page — so
// the two can never drift out of sync.

// HelpItem is one command and what it does.
type HelpItem struct {
	Cmd  string
	Desc string
}

// HelpGroup is a titled set of related commands.
type HelpGroup struct {
	Title string
	Items []HelpItem
}

// CommandHelp returns the public command reference, grouped for display.
func CommandHelp() []HelpGroup {
	return []HelpGroup{
		{"Playing", []HelpItem{
			{"!rpg", "Enroll, or show your character — then just idle: be present and quiet to level up."},
			{"!rpg status [name]", "A character sheet — yours, or a named player's."},
			{"!rpg sheet [name]", "The D&D ability block (STR/DEX/…), HP, gold, kills, draughts."},
			{"!rpg items (gear)", "Your equipped gear and total power."},
			{"!rpg stash [slot] / equip <#>", "Bank an equipped item, list your stash, or equip a stashed one."},
			{"!rpg info", "Realm summary: idlers online, the top player, the active quest."},
			{"!rpg who (online)", "Who's idling right now, highest level first."},
			{"!rpg top [kills|gold|duels]", "Leaderboards — by level (default), or kills/gold/duels."},
			{"!rpg feats [name]", "One-time achievements you've earned."},
			{"!rpg rebirth", "At level 50+, reset to level 0 for permanent prestige (★) and faster leveling — you keep gold, gear & glory."},
		}},
		{"Your character", []HelpItem{
			{"!rpg race <" + strings.Join(raceNames(), "|") + ">", "Choose your heritage once — bakes ability bonuses into your scores."},
			{"!rpg class <" + strings.Join(classNames(), "|") + ">", "Pick a class — sharpens your attacks and feeds your HP."},
			{"!rpg align <good|neutral|evil> [+ lawful|neutral|chaotic]", "Set your alignment on the D&D 9-point grid (affects combat)."},
			{"!rpg pet (companion)", "Your companion (a wolf, boar, hawk, imp, or owlbear — earned by slaying a boss)."},
		}},
		{"World & combat", []HelpItem{
			{"!rpg travel <town>", "Set off for a town; you walk there over the next ticks."},
			{"!rpg town", "Where you are on the map (at a town, travelling, or roaming)."},
			{"!rpg quest", "The active quest's party, objective, and time left."},
			{"!rpg duel <name>", "A friendly best-of-three spar with a present player."},
			{"!rpg give <name> <amount|item #>", "Give gold, or hand a stashed item, to another player."},
			{"!rpg rest / shop / buy <slot> / revive / bless", "Town services — heal, buy gear, revive, buy a combat blessing."},
			{"!rpg buy potion / quaff", "Buy a healing draught at a market; quaff one anywhere to heal full."},
			{"!rpg buy mount / mount", "Buy a steed at a market (double travel speed); show your mount."},
			{"!rpg enchant <slot>", "At a market, spend gold to push an item up one rarity tier."},
		}},
	}
}

// AdminHelp returns the privileged verbs (gated by the op flag / identity authz).
func AdminHelp() HelpGroup {
	return HelpGroup{"Admin — op flag", []HelpItem{
		{"!rpg pause / resume", "Freeze or resume the whole game."},
		{"!rpg push <name> <secs>", "Move a player's clock (negative = toward the next level)."},
		{"!rpg hog [name]", "Invoke the Hand of God on a named or random player."},
		{"!rpg raid", "Summon a world boss for the realm to fight (an event, or to test)."},
		{"!rpg setlevel <name> <n>", "Set a character's level."},
		{"!rpg gold <name> <amt>", "Grant or remove gold (negative removes)."},
		{"!rpg reset <name> | reset all yes", "Erase one character, or wipe the entire realm."},
	}}
}

// helpLine joins a group's command syntaxes into one terse channel line.
func helpLine(g HelpGroup) string {
	cmds := make([]string, len(g.Items))
	for i, it := range g.Items {
		cmds[i] = it.Cmd
	}
	return g.Title + " — " + strings.Join(cmds, " · ")
}

// help answers !rpg help in-channel: a terse command list (admins also see the
// privileged verbs), and a pointer to the full guide on the web dashboard.
func (m *Manager) help(msg engine.Message) {
	for _, g := range CommandHelp() {
		m.out.Say(msg.Network, msg.Channel, helpLine(g))
	}
	if m.authorized(msg) {
		m.out.Say(msg.Network, msg.Channel, helpLine(AdminHelp()))
	}
	m.out.Say(msg.Network, msg.Channel, "full guide (with what each command does) on the web dashboard → /help")
}

# IdleRPG

The classic IRC idle game, reborn. You "play" by being present in a channel and
**doing nothing**. Every tick you sit there quietly, you advance toward the next
level. Talking sets you back; leaving sets you back more. It's the same dumb,
compelling loop [idlerpg.net](http://idlerpg.net) ran for years — now cross-platform
and persistent.

Turn it on with the `idlerpg:` block (off by default):

```yaml
idlerpg:
  enabled: true
  interval: "60s"        # how often the game ticks
  base_ttl: "5m"         # time from level 0 to 1 (grows ~1.16x per level after)
  quest_interval: "6h"   # average gap between quests
  quest_duration: "1h"   # how long a quest runs
```

## Playing

- **`!rpg`** enrolls you (and shows your sheet after that). That's the whole
  opt-in. From then on, just *be in the channel*.
- **Leveling.** Each level has a time-to-go; idle it down to zero and you level
  up. Levels get longer the higher you climb (~1.16x each), capped so no single
  level takes more than 30 days.
- **Talking** adds time back — a few seconds per message, capped. Don't chat in a
  channel where you're trying to win.
- **Leaving** is worse: parting, quitting, getting kicked, or changing nick all
  add a penalty (a kick stings most). The bot follows your nick change but charges
  you for it.

After a bot restart, idlers already sitting in the channel are picked back up from
the NAMES list — you don't have to rejoin or speak to resume progress.

## What happens while you idle

- **Items.** On level-up you may find gear for one of ten slots (weapon, shield,
  ring, …). Each drop rolls a **rarity** (common → uncommon → rare → epic →
  legendary) that multiplies its power, and the rarest finds come **named** —
  epics get a slot-appropriate title (*Vicious Blade*, *Gilded Striders*) and
  legendaries a full epithet (*Whispering Greaves of Frost*). Names are drawn from
  per-slot pools, so two slots never collide. Better gear (by effective power)
  replaces worse; your total is your **power**. `!rpg items` shows your kit.
- **Battles.** Leveling up pits you against a random online player, weighted by
  power, with a chance of a critical hit. Win and your clock speeds up; lose and
  it slows (and takes HP).
- **Monster encounters.** At random a wandering idler runs into a level-scaled
  monster (giant rat → young dragon) and a quick d20 fight resolves — attack rolls
  vs AC, damage both ways. **Works solo** (unlike PvP battles). Win → time toward
  your next level, **gold**, a kill, maybe loot; lose → bloodied, or downed.
- **Bosses.** Rarely, a named legend rises instead — the Tarrasque, Tiamat,
  Asmodeus and their kin. They're level-gated and brutally tough (you'll often
  lose), but a kill is a windfall: a fortune in gold, several kills toward your
  titles, a big jump toward the next level, and **guaranteed top-tier loot**.
- **Events.** At random the gods intervene — a **godsend** (time forward), a
  **calamity** (time back, or an item loses its luster), or the **Hand of God**
  (a big swing either way).
- **Alignment** — the full D&D 9-point grid: an ethical axis (`!rpg align lawful|
  neutral|chaotic`) crossed with the moral axis (`good|neutral|evil`), or both at
  once (`!rpg align chaotic evil`). Good fights at +11% power and evil crits twice
  as often (PvP); lawful adds +AC and chaotic +attack in monster fights.
- **Race** (`!rpg race <human|elf|dwarf|…>`). Chosen once at creation; bakes small,
  permanent ability bonuses into your rolled scores (half-orc +2 STR, elf +2 DEX, …).
- **HP.** Derived from CON, level, and your class hit die. Losing a fight (and,
  later, monsters) deals damage; at 0 HP you're **downed** — no progress until you
  heal back, which happens a little each tick. `!rpg sheet` shows `HP cur/max`.
- **Class** (`!rpg class <fighter|ranger|rogue|cleric|bard|wizard>`). Mechanical:
  each keys off a primary ability (fighter STR, wizard INT, rogue/ranger DEX,
  cleric WIS, bard CHA) whose modifier is added to your attack power, whose hit die
  feeds your HP, and which grants a **signature combat ability** in monster fights —
  fighter's Extra Attack, wizard's Arcane Bolt, rogue's Sneak Attack, ranger's
  Hunter's Mark, cleric's Healing Word, bard's Cutting Words.
- **Titles.** As you rack up kills and levels you earn an honorific that rides
  next to your name — combat renown (the Brave → Slayer → Bloodied → Dragonslayer
  → Annihilator) and legend (the Seasoned → Veteran → Ascended → Mythic →
  Eternal). Your most prestigious earned title shows in your status and on the
  dashboard. Nothing to opt into; it's derived from your sheet.

## Quests

Every so often the gods draft a party of online idlers onto a quest (`!rpg quest`
shows the active one). There are two kinds, chosen at random:

- **Timed quest** — stay present and **silent** until the timer runs out, and the
  whole party's clock jumps forward.
- **Map quest** — the party *journeys* across the realm (a 500×500 grid) to two
  waypoints in sequence, moving a step each tick. Reaching the end wins. The web
  dashboard draws the map: the two waypoints, the route, and the party's moving
  position.

Either way it's a shared, fragile pact: if *any* quester talks, parts, quits, is
kicked, or changes nick, the quest collapses and the **entire party** is shoved
backward.

A quest survives a bot restart (its state is persisted), and the cadence is tunable
— drop `quest_interval` to `15m` and `quest_duration` to `5m` on a test channel if
you want to actually watch one play out.

## Towns

The world map's landmarks are functional stops. `!rpg travel <town>` heads you
toward one — you walk there over the next ticks (arrival is announced), and
`!rpg town` tells you where you are. While you're standing at a town you can use
its service:

- **Inn** (`!rpg rest`) — heal to full.
- **Market** (`!rpg shop`, then `!rpg buy <slot>`) — spend gold on a
  level-appropriate item. The gold sink for everything monsters drop.
- **Temple** (`!rpg revive`) — pay to clear the downed state and heal to full.

The dashboard character page shows where each character is (at a town, travelling,
or roaming).

## One character everywhere

Your character is keyed to your **account**, not your nick or platform. Link your
identities (see [accounts.md](accounts.md)) and you're a single hero whether you
idle from IRC or Discord — something the original idlerpg never did. Unlinked, you
get a per-network character, which is fine too.

## Running the game (admin / DM)

Admins (same identity auth as the console) get DM controls in-channel:
`!rpg pause` / `!rpg resume` freeze the game; `!rpg push <name> <secs>` and
`!rpg hog [name]` nudge fate; `!rpg setlevel <name> <n>` and `!rpg gold <name>
<amt>` adjust a character; **`!rpg reset <name>`** erases one character and
**`!rpg reset all yes`** wipes the whole realm for a fresh start.

## A dedicated IdleRPG bot

Want a "legit" game bot that *only* runs IdleRPG — no triggers, no ambient
chatter, no Markov babble, no karma/dice/8-ball, no quotes? You can carve all of
that away. Two switches do it:

1. **Turn off the optional command subsystems.** Each defaults on; set it off:

   ```yaml
   games:    { enabled: false }   # name++ / !karma / !roll / !8ball
   tell:     { enabled: false }   # !message <nick> …
   accounts: { enabled: true }    # keep — it's how a player is one character everywhere
   idlerpg:  { enabled: true }
   ```

2. **Leave the personality empty / disabled** so the engine never speaks — no
   triggers, and `enabled: false` on interjections, quotes, banter, and Markov,
   plus `commands: false`.

The result reacts to `!rpg` and nothing else. A complete, ready-to-edit example
is in [`configs/idlerpg.yaml`](../configs/idlerpg.yaml) — copy it, point it at
your network, and run it like any other bot (or as a service in the Compose
stack). It still needs Redis for game state.

## The web dashboard

A separate read-only service renders the realm as a web page — the top idlers, the
active quest, per-character pages (`/p/<name>`), and a **world map** (`/map`) where
every player wanders as a dot and the towns are marked. It reads the same Redis the
bots write, so it's just a view; it never touches the game. Auto-refreshes.

- **Docker Compose:** it's the `dashboard` service — browse <http://localhost:8080>.
- **Kubernetes:** `kubectl apply -k deploy/k8s/dashboard -n annoybots`, then
  `kubectl -n annoybots port-forward svc/rpg-dashboard 8080:80` (or wire an Ingress).

It needs the shared Redis, so IdleRPG state only shows up when `botnet.enabled:
true`. With state in-memory there's nothing for the dashboard to read.

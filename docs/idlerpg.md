# IdleRPG

The classic IRC idle game, reborn — and grown into a small D&D-flavored RPG that
plays itself while you do nothing. You "play" by being present in a channel and
**staying quiet**. Every tick you sit there in silence, you advance toward the
next level. Talking sets you back; leaving sets you back more. It's the same dumb,
compelling loop [idlerpg.net](http://idlerpg.net) ran for years — now cross-platform,
persistent, and layered with monsters, bosses, loot, towns, quests, companions,
duels, titles, and a live web dashboard.

The whole thing is **opt-in and zero-effort**: type `!rpg` once and then forget
about it. Everything below happens on its own.

## Contents

- [Quick start](#quick-start)
- [How leveling works](#how-leveling-works)
- [Your character](#your-character) — abilities, race, class, alignment, HP
- [Combat & the wilds](#combat--the-wilds) — monsters, biomes, bosses, companions, battles, duels, events
- [Loot, gear & power](#loot-gear--power) — rarity, magic names, enchanting
- [Gold, towns & potions](#gold-towns--potions)
- [Progression](#progression) — titles & feats
- [The world map & quests](#the-world-map--quests)
- [Rebirth (prestige)](#rebirth-prestige)
- [Leaderboards](#leaderboards)
- [The web dashboard](#the-web-dashboard)
- [One character everywhere](#one-character-everywhere)
- [Admin / Dungeon-Master controls](#admin--dungeon-master-controls)
- [Running a dedicated IdleRPG bot](#running-a-dedicated-idlerpg-bot)
- [Configuration](#configuration)

The full command list is in [commands.md](commands.md), or type **`!rpg help`** in
channel (the dashboard mirrors it at `/help`).

---

## Quick start

1. **`!rpg`** — enroll. You're level 0. Running it again shows your character.
2. **Be present and quiet.** Idle the channel; your level's timer ticks down.
3. *(optional, once)* pick a **`!rpg race`** and **`!rpg class`**, and set your
   **`!rpg align`**ment. These shape your combat and HP.
4. Watch the drama unfold — monster fights, loot, level-ups — in channel and on
   the [web dashboard](#the-web-dashboard).

That's it. Everything else is discovery.

Turn the game on with the `idlerpg:` config block (off by default) — see
[Configuration](#configuration).

## How leveling works

- **Leveling.** Each level has a time-to-go; idle it down to zero and you level
  up. Levels get longer the higher you climb (~1.16× each), capped so no single
  level ever takes more than 30 days.
- **Fellowship.** Idling isn't lonely: when several heroes are online together,
  everyone levels a little faster (+5% per companion, up to +30%). Company is
  strictly better — `!rpg who` shows the current bonus.
- **Talking is punished — visibly.** Any message that isn't an `!rpg` command adds
  time to your level clock, *scaled to your level*: a few seconds for a newbie, but
  hours for a high-level legend (it's a slice of your current level's duration). The
  bot **privately notifies you** of each penalty (`🤐 quiet — +Xs to your next
  level`) via an IRC NOTICE — a personal nudge, not channel spam, just like the
  original IdleRPG.
  Don't chat where you're trying to win.
- **Leaving** is worse: parting, quitting, getting kicked, or changing nick all
  add a penalty (a kick stings most). The bot follows your nick change but charges
  you for it.
- **Restart-proof.** After a bot restart, idlers already sitting in the channel
  are picked back up from the NAMES list — you don't have to rejoin or speak to
  resume progress. Your whole character persists in the shared store.

## Your character

`!rpg sheet` shows your full D&D character block. `!rpg status` shows the
one-line summary (and your title).

- **Abilities.** Six scores — STR, DEX, CON, INT, WIS, CHA — rolled at creation
  (4d6-drop-lowest) with the usual modifiers. They feed combat, HP, and class.
- **Race** (`!rpg race <human|elf|dwarf|halfling|half-orc|gnome|tiefling>`).
  Chosen once at creation; bakes small, permanent ability bonuses into your rolled
  scores (half-orc +2 STR, elf +2 DEX, …).
- **Class** (`!rpg class <fighter|ranger|rogue|cleric|bard|wizard>`). Mechanical:
  each keys off a primary ability (fighter STR, wizard INT, rogue/ranger DEX,
  cleric WIS, bard CHA) whose modifier sharpens your attacks and whose hit die
  feeds your HP — and each grants a **signature combat ability** in monster
  fights: fighter's Extra Attack, wizard's Arcane Bolt, rogue's Sneak Attack,
  ranger's Hunter's Mark, cleric's Healing Word, bard's Cutting Words.
- **Alignment** — the full D&D 9-point grid. An ethical axis (`!rpg align lawful|
  neutral|chaotic`) crossed with a moral axis (`good|neutral|evil`), or set both
  at once (`!rpg align chaotic evil`). Good fights at +11% power and evil crits
  twice as often (PvP); lawful adds +AC and chaotic +attack in monster fights.
- **HP.** Derived from CON, level, and your class hit die. Losing a fight deals
  damage; at 0 HP you're **downed** — no progress until you heal back (a little
  each tick, or instantly with a [healing draught](#gold-towns--potions) or at a
  temple). `!rpg sheet` shows `HP cur/max`.

## Combat & the wilds

- **Monster encounters.** At random a wandering idler runs into a level-scaled
  monster (giant rat → young dragon) and a quick d20 fight resolves — attack rolls
  vs AC, damage both ways. **Works solo** (unlike the PvP battle below). Win → time
  toward your next level, **gold**, a kill, maybe loot; lose → bloodied, or downed.
- **Terrain matters.** The land around each town is a biome — the coast (Lurk
  Harbor), the peaks (Mount AFK), the forest (Quietford), the swamp (The Lag
  Marsh), the plains. Common foes roam anywhere, but each biome has its own
  specialists: sahuagin and giant crabs by the sea, griffons and stone giants in
  the mountains, dire wolves and hags in the woods, bog zombies and will-o'-wisps
  in the marsh, bandits and manticores on the plains. **Where you wander shapes
  what you fight.**
- **Bosses.** Rarely, a named legend rises instead — the Tarrasque, the Kraken,
  the Lich-King, Tiamat, Asmodeus. They're level-gated and brutally tough (you'll
  often lose), and some are territorial (the Kraken only at the coast, Tiamat only
  in the peaks). But a kill is a windfall: a fortune in gold, several kills toward
  your titles, a big jump toward the next level, **guaranteed top-tier loot**, and
  sometimes a companion.
- **Companions.** Slay a boss and a beast may take to you — a wolf, dire boar,
  hawk, imp, or owlbear. Your companion fights at your side in every monster
  encounter, adding a small passive bonus to your attack and damage. `!rpg pet`
  shows yours; it also rides on your dashboard page.
- **PvP battles.** Leveling up pits you against a random online player, weighted by
  power, with a chance of a critical hit. Win and your clock speeds up; lose and it
  slows (and takes HP). This is automatic — it just happens on level-up.
- **Trading.** Share the wealth: `!rpg give <name> <amount>` hands gold to another
  player, and `!rpg give <name> item <#>` passes a stashed item into their stash.
  A straight transfer — help a friend or hand down gear.
- **Duels.** `!rpg duel <name>` challenges a present player to a friendly
  best-of-three spar — effective power (gear + alignment + class) plus luck. It's
  bragging rights only: no clock changes, no HP, no rewards, just a career win
  tally. Can't be farmed, only enjoyed.
- **Events.** At random the gods intervene — a **godsend** (time forward), a
  **calamity** (time back, or an item loses its luster), or the **Hand of God**
  (a big swing either way).
- **Weather.** Each biome has its own sky, rotating every so often: **fog** makes
  blows harder to land there (-attack), a **storm** steadies your foes (+their
  attack), and **rain** or **snow** slow the road through it. `!rpg weather` shows
  the sky everywhere; the dashboard map carries a legend.
- **Guilds.** `!rpg guild create <name>` founds a band of heroes for 500g; others
  `!rpg guild join <name>`. A guild's level is the sum of its members' levels, its
  vault is a shared purse (`!rpg guild deposit <gold>`), and whenever guildmates
  idle side by side each of them levels a little faster — +3% per guildmate present,
  up to +15%, stacking with the fellowship bonus. `!rpg guilds` ranks them; the Hall
  of Fame carries a guild table.
- **Dungeons.** While roaming you may stumble on a way down — the Sunken Crypt,
  the Gloomforge, the Barrow of Kings. A delve is personal and runs over several
  ticks: each one clears a room (a lurking foe, a sprung trap, or a chest), and
  the final chamber holds the **dungeon's lord**, worth a heavy purse and
  guaranteed treasure — and the **Delver** feat. Get downed inside and you're dragged
  out, the delve lost.
  You stop wandering while underground. `!rpg dungeon` shows your progress, and it
  appears on your dashboard page.
- **World events.** Now and then the whole realm shifts for a while: a **Blood
  Moon** (monsters prowl far more often), the **Harvest Festival** (every coin is
  worth half again), or a **Storm of Fate** (the gods meddle far more often).
  Announced when it falls and when it passes; `!rpg info` and the dashboard show
  the active one and its time left.
- **The wandering merchant.** Sometimes a passing trader finds you and gifts a
  purse of gold, a healing draught, or a shortcut toward your next level — a little
  kindness amid the monsters.
- **Blessing.** The flip side of poison, and a temple service: `!rpg bless`
  spends gold for a blessing that lasts a handful of ticks, adding +attack and
  +damage to your monster fights. Stack it before you go hunting a boss. `!rpg
  sheet` flags it (🕊️).
- **World bosses.** Rarely the whole realm is called to arms: a colossus —
  Bahamut, the Kraken Sovereign, Acererak — rises with a vast shared HP pool and
  a time limit. **Every** online idler automatically strikes it each tick, and the
  dashboard shows a live HP bar. Bring it down before it departs and *all*
  participants share the spoils (gold, a great leap toward the next level, a kill). The **top damage-dealer** also claims a champion's purse — so it pays to hit hard.
  Admins can summon one on demand with `!rpg raid`.
- **Poison.** Some foes are venomous — will-o'-wisps, green hags, bog zombies,
  sahuagin, wyverns, manticores. When one draws blood it leaves you **poisoned**,
  a damage-over-time that saps a little HP each tick until it wears off — or until
  you heal to full (a quaffed draught, an inn rest, or a temple revive all purge
  it). It makes the swamp and forest genuinely dangerous. `!rpg sheet` flags it
  (`HP 40/40☠️`), as does your dashboard page.

## Loot, gear & power

- **Items & rarity.** On level-up (and from monster kills) you may find gear for
  one of ten slots (weapon, shield, ring, …). Each drop rolls a **rarity** (common
  → uncommon → rare → epic → legendary) that multiplies its power. Better gear (by
  effective power) replaces worse; your total is your **power**. `!rpg items`
  shows your kit.
- **Magic names.** The rarest finds come **named** — epics get a slot-appropriate
  title (*Vicious Blade*, *Gilded Striders*) and legendaries a full epithet
  (*Whispering Greaves of Frost*). Names are drawn from per-slot pools, so two
  slots never collide and the same name rarely repeats.
- **Salvage.** When a better item replaces the one in a slot, the old gear is
  **salvaged into scrap gold** (scaled to its power) instead of being discarded —
  so every drop is worth something, even the ones you don't keep.
- **Stash.** `!rpg stash <slot>` banks an equipped item and frees the slot; `!rpg
  stash` lists what you've banked; `!rpg equip <#>` puts a stashed item back on
  (swapping the current one into the stash). Hold a spare legendary, or swap gear
  for different fights. (Auto-upgrades still salvage the old item for gold — the
  stash is for *deliberate* banking.)
- **Enchanting.** `!rpg enchant <slot>` at a market spends gold to push an equipped
  item up **one rarity tier**. Drops are random; enchanting is deterministic agency
  over your gear — the high-end gold sink, with a steep escalating price so a
  fortune in boss gold has somewhere to go. Reaching epic/legendary renames the
  item.

## Gold, towns & potions

Monster and boss kills mint **gold**. The world map's landmarks are functional
stops where you spend it. `!rpg travel <town>` heads you toward one — you walk
there over the next ticks (arrival is announced) — and `!rpg town` tells you where
you are. While standing at a town you can use its service:

- **Inn** (`!rpg rest`) — heal to full.
- **Market** (`!rpg shop`, then `!rpg buy <slot>`) — buy a level-appropriate item.
  Also home to **`!rpg enchant <slot>`** and **`!rpg buy potion`**.
- **Temple** (`!rpg revive`, `!rpg bless`) — clear the downed state and heal to
  full, or buy a temporary combat blessing.

**Healing draughts** are portable: buy them at a market (`!rpg buy potion`), carry
a stack, and `!rpg quaff` one **anywhere** to restore full HP — the only way to
pick yourself up after being downed far from a temple. Your stock shows on
`!rpg sheet`.

## Progression

Two long-term tracks build alongside your level:

- **Titles.** As you rack up kills and levels you earn an honorific that rides next
  to your name — combat renown (the Brave → Slayer → Bloodied → Dragonslayer →
  Annihilator) and legend (the Seasoned → Veteran → Ascended → Mythic → Eternal).
  Your most prestigious earned title shows in your status, on the leaderboard, and
  on your dashboard page. It's derived from your sheet — nothing to opt into.
- **Feats.** One-time achievements you cross exactly once — First Blood (first
  kill), Centurion (100 kills), Warlord (1000), Giant-Slayer (a boss falls),
  Treasure Hunter (a legendary), Deep Pockets (1000 gold). Each is announced the
  moment you earn it; `!rpg feats` lists yours and they badge your dashboard page.

## Rebirth (prestige)

Reached level 50? `!rpg rebirth` lets you **ascend**: your level and leaderboard
rank reset to 0, but you **keep** your gold, gear, kills, feats, and titles — and
gain a **permanent +5% leveling speed** per rebirth (up to +50%), plus prestige
stars (★) that ride by your name in channel and on the dashboard. It's a
new-game-plus for the truly dedicated: give up your rank for a faster, stronger
climb the next time around.

## The world map & quests

The realm is a 500×500 map. Every online player wanders a step each tick (it's
cosmetic — movement doesn't affect leveling, but it *does* decide your
[biome](#combat--the-wilds)). The [dashboard map](#the-web-dashboard) draws
everyone as a dot, with the towns and their terrain marked.

Every so often the gods draft a party of online idlers onto a **quest**
(`!rpg quest` shows the active one). Two kinds, chosen at random:

- **Timed quest** — stay present and **silent** until the timer runs out, and the
  whole party's clock jumps forward.
- **Hunt quest** — the party must *collectively slay a target number of monsters*
  before the timer runs out. Every monster any member fells counts toward the
  shared total; `!rpg quest` and the dashboard show live progress. Reach it in
  time to win; run out the clock and the hunt fails.
- **Map quest** — the party *journeys* to two waypoints in sequence, moving a step
  each tick. Reaching the end wins; the dashboard draws the route and the party's
  moving position.

Either way it's a shared, fragile pact: if *any* quester talks, parts, quits, is
kicked, or changes nick, the quest collapses and the **entire party** is shoved
backward. Quests survive a bot restart, and the cadence is tunable (see
[Configuration](#configuration)).

## Leaderboards

`!rpg who` lists who's idling right now (highest level first) — handy for finding
someone to duel, trade with, or party up.



`!rpg top` ranks by level. Add a category to rank differently:
`!rpg top kills`, `!rpg top gold`, or `!rpg top duels`. The web dashboard shows
the level leaderboard with titles, and a realm-wide stats overview.

## The web dashboard

A separate read-only service renders the realm as a web page. It reads the same
Redis the bots write, so it's just a view — it never touches the game, and
auto-refreshes every 30s. Pages:

- **Home (`/`)** — the realm-stats overview (heroes, levels gained, monsters slain,
  bosses felled, gold minted, legendaries), the level leaderboard with titles, the
  active quest, and a **live activity feed** of the realm's dramatic moments
  (level-ups, kills, boss fights, deaths, duels, feats, legendary finds) so the
  world has a visible memory beyond the channel.
- **Map (`/map`)** — the fantasy world map: every player as a wandering dot, the
  towns and their biomes, and an active map-quest's route.
- **Character (`/p/<name>`)** — a hero's full page: title, abilities, HP/gold/kills,
  duel wins, companion, draughts, equipment with rarity names, and earned feats.
- **Hall of Fame (`/hall`)** — every leaderboard side by side: top by level, kills,
  gold, duel wins, and rebirths.
- **How to play (`/help`)** — the full command reference (same source as
  `!rpg help`).

Running it:

- **Docker Compose:** it's the `dashboard` service — browse <http://localhost:8080>.
- **Kubernetes:** `kubectl apply -k deploy/k8s/dashboard -n annoybots`, then
  `kubectl -n annoybots port-forward svc/rpg-dashboard 8080:80` (or wire an Ingress).

It needs the shared Redis, so IdleRPG state only shows up when `botnet.enabled:
true`. With state in-memory there's nothing for the dashboard to read.

## One character everywhere

Your character is keyed to your **account**, not your nick or platform. Link your
identities (see [accounts.md](accounts.md)) and you're a single hero whether you
idle from IRC or Discord — something the original idlerpg never did. Unlinked, you
get a per-network character, which is fine too.

## Admin / Dungeon-Master controls

Bot admins (matched by the same verified identity as the admin console — the op
flag) get Dungeon-Master controls. **These are typed in channel**, like all `!rpg`
commands — "DM" here means Dungeon Master, not direct message — but only an admin
is heeded; everyone else gets a brush-off.

- `!rpg pause` / `!rpg resume` — freeze or resume the whole game.
- `!rpg push <name> <secs>` — move a player's clock (negative = toward the next level).
- `!rpg hog [name]` — invoke the Hand of God on a named or random player.
- `!rpg setlevel <name> <n>` — set a character's level.
- `!rpg gold <name> <amt>` — grant or remove gold.
- `!rpg reset <name>` — erase one character.
- `!rpg reset all yes` — wipe the **entire** realm for a fresh start (the `yes` is
  a required guard).

## Running a dedicated IdleRPG bot

Want a "legit" game bot that *only* runs IdleRPG — no triggers, no ambient chatter,
no Markov babble, no karma/dice/8-ball, no quotes? Carve all of that away with two
switches:

1. **Turn off the optional command subsystems** (each defaults on):

   ```yaml
   games:    { enabled: false }   # name++ / !karma / !roll / !8ball
   tell:     { enabled: false }   # !message <nick> …
   accounts: { enabled: true }    # keep — it's how a player is one character everywhere
   idlerpg:  { enabled: true }
   ```

2. **Leave the personality empty / disabled** so the engine never speaks — no
   triggers, and `enabled: false` on interjections, quotes, banter, and Markov,
   plus `commands: false`.

The result reacts to `!rpg` and nothing else. A complete, ready-to-edit example is
in [`configs/idlerpg.yaml`](../configs/idlerpg.yaml) — copy it, point it at your
network, and run it like any other bot (or as a service in the Compose stack). It
still needs Redis for game state.

## Configuration

```yaml
idlerpg:
  enabled: true
  interval: "60s"        # how often the game ticks
  base_ttl: "5m"         # time from level 0 to 1 (grows ~1.16x per level after)
  quest_interval: "6h"   # average gap between quests
  quest_duration: "1h"   # how long a quest runs
```

For a test channel where you want to *watch* things happen, shrink the timers —
e.g. `interval: "5s"`, `base_ttl: "30s"`, `quest_interval: "15m"`,
`quest_duration: "5m"`. Game state lives in the shared store, so set
`botnet.enabled: true` (Redis) for anything to persist or appear on the dashboard.

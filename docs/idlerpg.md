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
  ring, …). Better gear replaces worse; the sum of your item levels is your
  **power**. `!rpg items` shows your kit.
- **Battles.** Leveling up pits you against a random online player, weighted by
  power, with a chance of a critical hit. Win and your clock speeds up; lose and
  it slows.
- **Events.** At random the gods intervene — a **godsend** (time forward), a
  **calamity** (time back, or an item loses its luster), or the **Hand of God**
  (a big swing either way).
- **Alignment** (`!rpg align good|neutral|evil`). Good fights at +11% power; evil
  crits twice as often; neutral is baseline.
- **Class** (`!rpg class <name>`). Pure flavor — it shows up in your status line.

## Quests

Every so often the gods draft a party of online idlers onto a **timed quest**
(`!rpg quest` shows the active one). The deal is simple: stay present and **silent**
until the timer runs out and the whole party's clock jumps forward. If *any*
quester talks, parts, quits, is kicked, or changes nick, the quest collapses and
the entire party is shoved backward — so a quest is a shared, fragile pact.

A quest survives a bot restart (its state is persisted), and the cadence is tunable
— drop `quest_interval` to `15m` and `quest_duration` to `5m` on a test channel if
you want to actually watch one play out.

## One character everywhere

Your character is keyed to your **account**, not your nick or platform. Link your
identities (see [accounts.md](accounts.md)) and you're a single hero whether you
idle from IRC or Discord — something the original idlerpg never did. Unlinked, you
get a per-network character, which is fine too.

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

A separate read-only service renders the realm as a web page — the top idlers and
the active quest, auto-refreshing. It reads the same Redis the bots write, so it's
just a view; it never touches the game.

- **Docker Compose:** it's the `dashboard` service — browse <http://localhost:8080>.
- **Kubernetes:** `kubectl apply -k deploy/k8s/dashboard -n annoybots`, then
  `kubectl -n annoybots port-forward svc/rpg-dashboard 8080:80` (or wire an Ingress).

It needs the shared Redis, so IdleRPG state only shows up when `botnet.enabled:
true`. With state in-memory there's nothing for the dashboard to read.

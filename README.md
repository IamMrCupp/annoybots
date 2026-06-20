# annoybots

A framework for chat "nuisance" bots — the modern, containerized descendant of
the classic [BMotion](http://bmotion.sourceforge.net/) IRC bots, rebuilt in Go
for Kubernetes.

One small Go binary holds many simultaneous chat connections at once (IRC
networks, Twitch, and Discord) and routes every channel through a shared
"annoyance engine." **Each bot is the same binary with a different personality
file**, so you can run as many bots as you like — each with its own name,
triggers, and voice. The repo ships two example personalities, **Echo** and
**Mimic**, to copy from.

## What it does

- **Keyword/regex triggers** → randomized, templated responses (`{nick}`, `{me}`, `{chan}`, capture groups).
- **Ambient interjections** — random unprompted lines, with per-channel cooldowns.
- **Quote packs** — drop-in `.txt` files surfaced randomly and via `!quote [pack]`; `!packs` lists what's available (same as the `/quote`, `/packs` slash commands on Discord).
- **A learning "brain"** — an order-N Markov chain that learns from channel chatter and babbles it back, mangled. It persists to disk so learning survives restarts (just like the BMotion babble everyone remembers, minus the abandoned TCL stack).
- **Multi-network in one process** — IRC + Twitch share the same wire protocol; Twitch just needs CAP negotiation, an `oauth:` token, and tighter rate limits, all handled automatically. Discord rides alongside on the same engine.
- **Many bots, one binary** — personality is config, not code, so a new bot is a new YAML file (and a pod), never a fork.
- **Games & toys** — karma (`name++` / `name--` / `!karma` / `!top`), `!roll`, `!8ball`, and a full **[IdleRPG](docs/idlerpg.md)** (idle to level, items, battles, random events, alignment/classes, and timed party **quests**) with a read-only **web dashboard**.
- **Cross-network accounts** — `!register` / `!link` so one person is one identity (and one IdleRPG hero) whether they're on IRC or Discord. See [docs/accounts.md](docs/accounts.md).
- **Leave a message** — `!message <nick> <text>`, delivered when they're next around.
- **Channel keeping** — eggdrop-style: an opped bot keeps its sibling bots opped.
- **Chat admin console** — DM the bot to puppet it, edit quotes, manage channels and admins; identity-authenticated, tiered by access flag.
- **Lua plugins** — eggdrop-style scripting: drop a `.lua` file to add `!commands` with no rebuild. See [docs/plugins.md](docs/plugins.md).

For the full command list see **[docs/commands.md](docs/commands.md)**.

## Layout

```
cmd/annoybot         entrypoint
cmd/dashboard        read-only IdleRPG web dashboard (separate binary, same image)
internal/engine      annoyance engine (triggers, interjections, quotes, commands)
internal/markov      persistent Markov "brain"
internal/bot         transport router (fans replies back to the right platform)
internal/botnet      inter-bot bus (Redis pub/sub) + skit coordinator
internal/event       transport-agnostic event dispatcher (joins/parts/quits/…)
internal/state       shared state store (Redis or in-memory) for karma/accounts/IdleRPG
internal/irc         IRC/Twitch transport (ergochat/irc-go) + per-network rate limiting
internal/discord     Discord transport (bwmarrin/discordgo) + slash commands
internal/admin       chat admin console (identity auth, access flags, partyline)
internal/games       karma, !roll, !8ball
internal/idlerpg     the IdleRPG game
internal/rpgweb      HTML rendering for the dashboard
internal/account     cross-network identity linking
internal/tell        "!message <nick>" deferred delivery
internal/plugin      eggdrop-style Lua scripting (command binds)
internal/ratelimit   token-bucket limiter (Twitch-aware)
internal/cooldown    per-channel cooldown tracking
internal/config      YAML config loading, validation, quote-pack loading
internal/health      /healthz + /readyz for Kubernetes probes
configs/             echo.yaml, mimic.yaml — example personalities (+ networks)
data/quotes/         starter quote packs
data/skits.yaml      shared multi-bot skit scripts
data/plugins/        example Lua plugins
deploy/compose/      Docker Compose stack (no Kubernetes needed)
deploy/k8s/          kustomize base + per-bot overlays + the dashboard
docs/                command, IdleRPG, and accounts reference
```

## Quick start (Docker Compose)

No Kubernetes required. The [`deploy/compose`](deploy/compose) stack runs a bot,
the Redis it uses for state, and the IdleRPG dashboard:

```sh
cd deploy/compose
cp .env.example .env          # fill in your tokens
$EDITOR bot.yaml              # set your networks, channels, admins
docker compose up -d
```

The dashboard comes up at <http://localhost:8080>. See
[`deploy/compose/README.md`](deploy/compose/README.md) for the details.

## Run locally (from source)

```sh
# Export the secrets your config references first, e.g.:
export ECHO_LIBERA_SASL=...           # NickServ/SASL password
export ECHO_TWITCH_TOKEN=oauth:...    # Twitch chat token

make run-echo                          # or run-mimic
# any config:  go run ./cmd/annoybot -config configs/yourbot.yaml
```

Quote-pack files resolve relative to `ANNOYBOT_QUOTES_DIR` (the Makefile points
it at `data/quotes`).

## Configuration

Everything behavioral lives in `configs/<bot>.yaml`: networks, triggers,
interjection/quote rates and cooldowns, and Markov settings. **Secrets are never
stored in the config** — each `*_env` field names an environment variable
(populated from a Kubernetes Secret) that holds the actual token or password.
Start from `configs/echo.yaml` or `configs/mimic.yaml`; both exercise every
feature and all three platforms.

**Feature toggles.** The optional command subsystems — `games` (karma/dice/8-ball),
`tell` (`!message`), and `accounts` — each default **on**; set `enabled: false` on
any to carve it out. Combined with an empty/disabled personality, that's how you
run a single-purpose bot: [`configs/idlerpg.yaml`](configs/idlerpg.yaml) is a
ready-made **IdleRPG-only** bot that runs the game and stays silent on everything
else. See [docs/idlerpg.md](docs/idlerpg.md#a-dedicated-idlerpg-bot).

**State persistence.** Karma, accounts, and IdleRPG live in a shared state store.
That store is **Redis when `botnet.enabled: true`** — so state persists across
restarts and is shared across all your bots — and **in-memory otherwise** (handy
for a quick local run, but it resets when the process stops). The Docker Compose
stack and the k8s `deploy/k8s/redis` both stand Redis up for you.

### Add a bot

A bot is just the binary pointed at a different config, so adding one is purely
additive — no code changes:

1. **Copy an example config.** `cp configs/echo.yaml configs/jeeves.yaml`, then
   set `bot: jeeves` and `personality.name: "Jeeves"` and edit the networks,
   triggers, banter, and quote packs to taste.
2. **Name its secrets.** Each `*_env` field names an env var. For Kubernetes,
   collect them into a `jeeves-bot-secrets` Secret (see [Deploy](#deploy)).
3. **Introduce it to the others.** Add `"Jeeves"` to every other bot's
   `personality.siblings`, and optionally give it lines in `data/skits.yaml`
   (skit steps are matched by the `bot:` value, e.g. `bot: jeeves`).
4. **Deploy it.** Copy `deploy/k8s/overlays/echo/` to `…/jeeves/`: point the
   `bot-config` generator at `configs/jeeves.yaml` and the secretRef patch at
   `jeeves-bot-secrets`. Add a Flux `Kustomization` (copy a block in
   `deploy/k8s/flux/kustomizations.yaml`).

The engine, botnet bus, and admin console all work the same for any number of bots.

### Twitch

Set `kind: twitch` on a network and point `password_env` at an env var holding a
chat oauth token. Server, TLS, CAPs, and conservative rate limits default
automatically. Note Twitch does not reliably broadcast joins/parts or mode
changes, so user/op tracking there is intentionally not relied upon.

### Discord

Set `kind: discord` on a network and point `password_env` at an env var holding
the **bot token** (no nick/server needed). Then:

1. In the [Discord developer portal](https://discord.com/developers/applications),
   create an application + bot, and **enable the privileged MESSAGE CONTENT
   intent** under Bot → Privileged Gateway Intents. Without it the bot connects
   but every message body arrives empty.
2. Invite the bot to your server with an OAuth2 URL using the `bot` and
   `applications.commands` scopes.
3. List your server's ID under `guilds:` for instant `/quote` and `/annoy` slash
   commands (global registration works too but can take up to an hour to appear).

`channels` is an optional allowlist of channel IDs; empty means the bot responds
everywhere it can see. Discord's own HTTP rate limits are handled by the client
library, so the token-bucket limiter is IRC/Twitch-only. The same triggers,
quote packs, and Markov brain run on Discord unchanged; IRC `/me` actions render
as italics.

## Bot-to-bot interaction (the "botnet")

Like the old eggdrop botnet BMotion used for coordinated trolling, the bots can
talk to each other — but safely.

**Banter (cross-talk).** Each bot lists the others as `siblings`. A sibling's
messages can *only* produce capped banter — never normal triggers — so bots can
never trigger each other into an infinite flood. Banter is bounded twice: a
per-channel cooldown *and* a hard "max replies per rolling window" cap.

**Skits.** Multi-bot scripted bits live in a shared `data/skits.yaml` loaded by
every sibling bot. They coordinate over a Redis pub/sub bus, so a skit works even
if the bots are on different platforms. The "lead" bot (owner of the first line)
initiates — via `!skit <name>` in a shared channel, or randomly via each skit's
`chance` — then the bots perform their lines in lockstep, bounded by the step
count and a per-channel cooldown. Add your own by editing `data/skits.yaml`
(each step's `bot:` must match a bot's `bot:` config value).

Enable it with the `botnet:` block in each bot's config (all bots must share the
same `channel`). Deploy the shared bus once:

```sh
kubectl apply -k deploy/k8s/redis
```

The bus carries only ephemeral coordination messages, so the Redis runs without
persistence.

**Federation.** Bots on *other* hosts (a VPS, a friend's box) can join the same
botnet by pointing at the same Redis over a private WireGuard/Headscale mesh — no
code changes, just `botnet.redis_addr` + a password. See
[docs/federation.md](docs/federation.md) and the [`deploy/remote`](deploy/remote) kit.

## Admin console (chat)

DM a bot to run admin commands. Admins are matched by **verified identity** — an
IRC services/NickServ account, a Discord user ID, or a Twitch login — never by
spoofable nick, and commands are only honored in DMs. Configure admins in the
`admin:` block of each bot's config.

**First-run claim (no password to manage).** If the console is enabled but no
admins are configured, the bot prints a one-time **claim code** to its log on
startup. The first person to DM `!claim <code>` (from a verified identity)
becomes the owner — their identity is recorded and the code is spent. Nothing to
invent, paste into a Secret, or keep around: it bootstraps straight into identity
auth. (The code lives only in memory, so a restart prints a fresh one until it's
claimed; set `admin.state_path` so the claimed owner persists.)

Commands (send `!help` for the list; full reference in [docs/commands.md](docs/commands.md)):

- `!networks` — which networks the bot is currently connected to
- `!join <net> <#chan>` / `!part <net> <#chan>` — channel ops
- `!invite <net> <#chan> <nick>` — IRC INVITE (bot needs ops on `+i` channels)
- `!say <net> <target> <text>` / `!act <net> <target> <text>` — puppet the bot (the target can be a nick or service, e.g. `!say <net> NickServ "IDENTIFY …"`)
- `!identify <net> [password]` — (re)authenticate to NickServ; omit the password to use the bot's configured secret (nothing sensitive typed in chat)
- `!addquote <pack> <text>` / `!delquote <pack> <text>` — runtime quote editing
- `!addadmin <net|*> <account>` / `!deladmin <net|*> <account>` / `!admins`
- `!reload` — re-read quote-pack files and the skits file from disk, no restart
  (network connections and personality triggers still require a restart)

Quote and admin changes persist to the data volume and **sync to the sibling bots
over the botnet bus**, so you only have to DM one of them. Channel control and
puppeting stay local to the bot you DM. (`!delquote` only removes runtime-added
lines; file-pack lines are immutable — edit the `.txt` to change those.)

**Password fallback.** Identity auth is primary, but if a network has no services
(or someone just isn't logged in), set `password_env` and an admin can
`!login <password>` in a DM to get a time-limited session (`!logout` to end it).
Heads up: without services this session is keyed by **nick**, which is spoofable,
so it's weaker than account-based auth — it's a convenience for when services are
unavailable, with a constant-time password check, a failed-attempt throttle, and
a configurable `session_ttl`. Leave `password_env` unset to disable it.

## Deploy

This is built for **GitOps with Flux**: Conventional Commits → `release-please`
cuts a semver release → CI builds the image → Flux's semver `ImagePolicy` rolls
it out. Secrets are **hand-applied by default** (SOPS-encrypted secrets-in-Git
are optional); quote/skit content is served from ConfigMaps so edits go live via
`!reload`. See [`deploy/k8s/flux/README.md`](deploy/k8s/flux/README.md) for the
full wiring (Kustomizations, image automation, and the optional SOPS/age setup).

Each bot deploys as a single-replica StatefulSet with its own PVC for the Markov
brain; Redis (the botnet bus *and* the shared state store for karma/accounts/
IdleRPG) deploys once from `deploy/k8s/redis`.

### IdleRPG web dashboard

If you run [IdleRPG](docs/idlerpg.md), deploy the read-only dashboard:

```sh
kubectl apply -k deploy/k8s/dashboard -n annoybots
kubectl -n annoybots port-forward svc/rpg-dashboard 8080:80   # then open http://localhost:8080
```

It's the same image with the `/dashboard` entrypoint, reads the shared Redis only
(no secrets), and shows the rankings and any active quest. Point an Ingress at the
`rpg-dashboard` Service to make it public.

### Manual apply (no Flux)

```sh
kubectl create namespace annoybots

# Hand-apply each bot's Secret — keys are listed in
# deploy/k8s/overlays/<bot>/secret.example.yaml. Omit a key to disable a network:
kubectl -n annoybots create secret generic echo-bot-secrets \
  --from-literal=ECHO_TWITCH_TOKEN='...' --from-literal=ECHO_DISCORD_TOKEN='...'
kubectl -n annoybots create secret generic mimic-bot-secrets \
  --from-literal=MIMIC_TWITCH_TOKEN='...' --from-literal=MIMIC_DISCORD_TOKEN='...'

kubectl apply -k deploy/k8s/redis
kubectl kustomize --load-restrictor LoadRestrictionsNone deploy/k8s/overlays/echo  | kubectl apply -n annoybots -f -
kubectl kustomize --load-restrictor LoadRestrictionsNone deploy/k8s/overlays/mimic | kubectl apply -n annoybots -f -
```

## Adding quotes

Drop a `whatever.txt` in `data/quotes/` (one quote per line, `#` comments
allowed), reference it in a bot's `personality.quotes.packs`, and list it in the
overlay's `bot-quotes` `configMapGenerator`. With Flux, commit it and `!reload`;
otherwise rebuild the image (packs are also baked in at `/quotes` as a fallback).

Some bundled packs are pop-culture one-liners (Futurama, South Park, …) in the
old BMotion tradition — credited, with their rights-holder note, in
[`data/quotes/CREDITS.md`](data/quotes/CREDITS.md). They're examples; swap in your
own if you'd rather not redistribute them.

## Develop

```sh
make test     # go test ./...
make lint     # golangci-lint
make docker   # build the image
```

## Acknowledgments

annoybots stands on the shoulders of the IRC bots that annoyed channels before it.
None of their code is used here — this is a clean-room rewrite in Go — but the
ideas, and a lot of the fun, are theirs:

- **[eggdrop](https://www.eggheads.org/)** — the original scriptable IRC bot; the
  flag/access model, partyline, and channel-keeping all trace back to it.
- **[BMotion](http://bmotion.sourceforge.net/)** — the eggdrop TCL framework whose
  ambient "the bot just talks" behavior, banter, and quote packs this is a love
  letter to.
- **[IdleRPG](http://idlerpg.net/)** (and the [falsovsky PHP fork](https://github.com/falsovsky/idlerpg))
  — the idle game reimagined in [`internal/idlerpg`](internal/idlerpg).

Built on [ergochat/irc-go](https://github.com/ergochat/irc-go),
[bwmarrin/discordgo](https://github.com/bwmarrin/discordgo), and
[redis/go-redis](https://github.com/redis/go-redis), among others — all permissively
licensed (MIT/BSD/Apache).

## License

[MIT](LICENSE) — do whatever you want, no warranty. The bundled pop-culture quote
packs are the exception: they belong to their respective rights holders and are
**not** covered by this license (see [`data/quotes/CREDITS.md`](data/quotes/CREDITS.md)).

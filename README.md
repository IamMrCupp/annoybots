# annoybots

Modern, containerized rebirth of two classic [BMotion](http://bmotion.sourceforge.net/)-style
IRC nuisance bots — **Arywen** and **Kurkutu** — for Kubernetes.

One small Go binary holds many simultaneous chat connections at once (real IRC
networks, a private InspIRCd test net, and Twitch) and routes every channel
through a shared "annoyance engine." Each bot is the same binary with a
different personality file.

## What it does

- **Keyword/regex triggers** → randomized, templated responses (`{nick}`, `{me}`, `{chan}`, capture groups).
- **Ambient interjections** — random unprompted lines, with per-channel cooldowns.
- **Quote packs** — drop-in `.txt` files (Rick & Morty, South Park, Futurama, Snuff Box, classic bot sass) surfaced randomly and via `!quote [pack]`; `!packs` lists what's available (same as the `/quote`, `/packs` slash commands on Discord).
- **A learning "brain"** — an order-N Markov chain that learns from channel chatter and babbles it back, mangled. It persists to disk so learning survives restarts (just like the BMotion babble everyone remembers, minus the abandoned TCL stack).
- **Multi-network in one process** — IRC + Twitch share the same wire protocol; Twitch just needs CAP negotiation, an `oauth:` token, and tighter rate limits, all handled automatically.

## Layout

```
cmd/annoybot         entrypoint
internal/engine      annoyance engine (triggers, interjections, quotes, commands)
internal/markov      persistent Markov "brain"
internal/bot         transport router (fans replies back to the right platform)
internal/botnet      inter-bot bus (Redis pub/sub) + skit coordinator
internal/irc         IRC/Twitch transport (ergochat/irc-go) + per-network rate limiting
internal/discord     Discord transport (bwmarrin/discordgo) + slash commands
internal/ratelimit   token-bucket limiter (Twitch-aware)
internal/cooldown    per-channel cooldown tracking
internal/config      YAML config loading, validation, quote-pack loading
internal/health      /healthz + /readyz for Kubernetes probes
configs/             arywen.yaml, kurkutu.yaml (personality + networks)
data/quotes/         starter quote packs
deploy/k8s/          kustomize base + per-bot overlays
```

## Run locally

```sh
# Export the secrets your config references first, e.g.:
export ARYWEN_LIBERA_SASL=...           # NickServ/SASL password
export ARYWEN_TWITCH_TOKEN=oauth:...    # Twitch chat token

make run-arywen
```

Quote-pack files resolve relative to `ANNOYBOT_QUOTES_DIR` (the Makefile points
it at `data/quotes`).

## Configuration

Everything behavioral lives in `configs/<bot>.yaml`: networks, triggers,
interjection/quote rates and cooldowns, and Markov settings. **Secrets are never
stored in the config** — each `*_env` field names an environment variable
(populated from a Kubernetes Secret) that holds the actual token or password.

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
messages can *only* produce capped banter — never normal triggers — so two bots
can never trigger each other into an infinite flood. Banter is bounded twice: a
per-channel cooldown *and* a hard "max replies per rolling window" cap.

**Skits.** Multi-bot scripted bits live in a shared `data/skits.yaml` loaded by
both bots. They coordinate over a Redis pub/sub bus, so a skit works even if the
bots are on different platforms. The "lead" bot (owner of the first line)
initiates — via `!skit <name>` in a shared channel, or randomly via each skit's
`chance` — then the bots perform their lines in lockstep, bounded by the step
count and a per-channel cooldown. Add your own skits by editing `data/skits.yaml`.

Enable it with the `botnet:` block in each bot's config (both must share the same
`channel`). Deploy the shared bus once:

```sh
kubectl apply -k deploy/k8s/redis
```

The bus carries only ephemeral coordination messages, so the Redis runs without
persistence.

## Admin console (chat)

DM a bot to run admin commands. Admins are matched by **verified identity** — an
IRC services/NickServ account, a Discord user ID, or a Twitch login — never by
spoofable nick, and commands are only honored in DMs. Configure admins in the
`admin:` block of each bot's config.

Commands (send `!help` for the list):

- `!join <net> <#chan>` / `!part <net> <#chan>` — channel ops
- `!invite <net> <#chan> <nick>` — IRC INVITE (bot needs ops on `+i` channels)
- `!say <net> <target> <text>` / `!act <net> <target> <text>` — puppet the bot
- `!addquote <pack> <text>` / `!delquote <pack> <text>` — runtime quote editing
- `!addadmin <net|*> <account>` / `!deladmin <net|*> <account>` / `!admins`

Quote and admin changes persist to the data volume and **sync to the sibling bot
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

## Deploy to Kubernetes

```sh
cd deploy/k8s/overlays/arywen
cp secret.example.env secret.env      # fill in real tokens; secret.env is gitignored

# The bot config lives in ../../../../configs, so disable the load restrictor:
kubectl kustomize --load-restrictor LoadRestrictionsNone . | kubectl apply -f -
```

Each bot deploys as a single-replica StatefulSet with its own PVC for the Markov
brain. Repeat for `overlays/kurkutu`.

## Adding quotes

Drop a `whatever.txt` file in `data/quotes/` (one quote per line, `#` comments
allowed), then reference it in a bot's `personality.quotes.packs`. Rebuild the
image (packs are baked in at `/quotes`) or mount them via a ConfigMap.

## Develop

```sh
make test     # go test ./...
make lint     # golangci-lint
make docker   # build the image
```

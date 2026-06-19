# Run annoybots with Docker Compose

The fastest way to run a bot without Kubernetes. This stack brings up three
containers: one **bot**, the **Redis** it uses for state and bot-to-bot coms, and
the read-only **IdleRPG dashboard**.

## Quick start

```sh
cd deploy/compose
cp .env.example .env          # fill in your tokens (Discord, IRC SASL, …)
$EDITOR bot.yaml              # set your networks, channels, and admins
docker compose up -d
```

The dashboard comes up at <http://localhost:8080>. Tail the bot with
`docker compose logs -f bot`.

## What's in the box

| Service | What it is |
|---|---|
| `bot` | The annoybot itself. Personality, networks, and features all come from `bot.yaml`. |
| `redis` | The shared store: karma, accounts, and IdleRPG state persist here, and it's the bus sibling bots coordinate over. Runs without on-disk persistence (the state that matters is small and game-only — back up the volume if you care about it). |
| `dashboard` | The same image with the `/dashboard` entrypoint: a read-only web view of the IdleRPG realm. |

Secrets live in `.env`, never in `bot.yaml` — each `*_env` field in the config
names a variable to read. Omit a variable to disable that network.

## Common changes

- **Add a second bot.** Copy the `bot` service to `bot2`, give it its own config
  (`./bot2.yaml:/config/bot.yaml:ro`), and list each bot in the others'
  `personality.siblings`. They'll coordinate skits over the shared Redis.
- **Make quests fire often** (to actually watch one): set `idlerpg.quest_interval`
  to something like `15m` and `quest_duration` to `5m` in `bot.yaml`.
- **Turn off the game or the dashboard.** Set `idlerpg.enabled: false`, or drop
  the `dashboard` service.
- **Persist nothing.** Remove the `bot-data` volume and set `brain.path` to a
  throwaway path — the bot starts fresh each time.

For the complete configuration surface, copy from
[`configs/echo.yaml`](../../configs/echo.yaml) (it exercises every feature and all
three platforms). For commands and the game, see [`docs/`](../../docs).

# Remote bot (federation)

Run a bot on another host (a VPS, a friend's box) that joins your **existing**
botnet — shared skits, partyline, banter, and one shared IdleRPG/karma/account
world. The full picture is in [docs/federation.md](../../docs/federation.md); this
is the on-the-box kit.

## Prerequisites

1. The home hub Redis is password-protected and reachable over your mesh (see the
   "Harden the hub" section of the federation guide).
2. This host has joined the **WireGuard/Headscale mesh** and can reach the hub's
   tunnel IP — `redis-cli -h 10.99.0.1 -a "$REDIS_PASSWORD" ping` returns `PONG`.

## Run it

```sh
cd deploy/remote
cp .env.example .env       # REDIS_PASSWORD + this bot's tokens
$EDITOR bot.yaml           # set botnet.redis_addr to the hub's mesh IP, a unique bot: name, its networks
docker compose up -d
docker compose logs -f bot # success looks like: "botnet bus connected"
```

Then add this bot's name to the **other** bots' `personality.siblings` so banter
and skits include it. Confirm the link with a `!party` from a home bot — the
partyline relays across the tunnel.

## Files

- `docker-compose.yml` — the bot only (no local Redis; it dials the hub).
- `.env.example` — `REDIS_PASSWORD` + this bot's tokens.
- `bot.yaml` — the remote bot's config; the `botnet:` block points at the hub.
- `wg0.conf.example` — a raw-WireGuard peer config (skip it if you use Headscale).

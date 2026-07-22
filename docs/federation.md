# Federation — bots on other hosts

You can run bots on other machines (a VPS, a friend's box) and have them join the
*same* logical botnet as your home bots: shared skits, partyline, cross-host
banter, and one shared IdleRPG/karma/account world. This is a **deploy and
networking** exercise — annoybots needs **no code changes** for it. The bot already
points at any Redis via `botnet.redis_addr` + `botnet.redis_password_env`.

## How it works

The "botnet" is literally **one Redis instance plus one matching `channel`**. Redis
pub/sub doesn't federate, so every bot — home or remote — connects to the *same*
Redis and subscribes to the *same* channel. That single connection is the whole
link. Because that same Redis also backs the shared state store, a federated bus
gives you shared game state across hosts for free.

```
   home bots ──┐
               ├──▶  Redis (the hub)  ◀──── remote bot (VPS)
  dashboard ───┘            ▲
                            └──── reached over a private WireGuard mesh
```

**Remote bots reach Redis over a private mesh, never the public internet.** Redis
speaks unauthenticated-friendly plaintext; exposing it to the world is a recipe for
getting owned. Put it on a WireGuard tunnel instead.

## 1. Harden the hub (home side)

Add a password and expose Redis beyond the cluster's ClusterIP. With the bundled
`deploy/k8s/redis`, that's two changes:

```yaml
# in the redis container: require a password (from a hand-applied secret)
args: ["--requirepass", "$(REDIS_PASSWORD)", "--save", "", "--appendonly", "no"]
env:
  - name: REDIS_PASSWORD
    valueFrom: { secretKeyRef: { name: redis-auth, key: REDIS_PASSWORD } }
---
# a second Service so the mesh can reach it (ClusterIP stays for home bots)
apiVersion: v1
kind: Service
metadata: { name: redis-ext, namespace: annoybots }
spec:
  type: NodePort
  selector: { app.kubernetes.io/name: annoybot-redis }
  ports: [{ port: 6379, targetPort: redis, nodePort: 31637 }]
```

Then give your **home** bots the same `REDIS_PASSWORD` env (they keep using
`redis_addr: redis:6379`). TLS to Redis is optional under WireGuard — the tunnel
already encrypts — and only becomes mandatory if Redis is ever exposed publicly
(which it shouldn't be).

> Not on k8s? Same idea: run Redis with `--requirepass`, bind it to the tunnel
> interface, and don't publish 6379 to the internet.

## 2. Stand up the mesh

Self-hosted, no paid service. Two options:

- **Headscale (recommended)** — a self-hosted Tailscale control server. You get NAT
  traversal, MagicDNS, and declarative ACLs, and onboarding a host is one
  `tailscale up --login-server https://your-headscale`. Best when the mesh also
  fronts other home services (NAS, media, a roaming laptop).
- **Raw WireGuard (no dependencies)** — forward one UDP port on your router, hand-
  manage keys and peers. Fewer moving parts; more manual. See
  [`deploy/remote/wg0.conf.example`](../deploy/remote/wg0.conf.example).

**Scope access with ACLs.** A remote bot only needs to reach Redis — nothing else
on your LAN. With raw WireGuard that's `AllowedIPs` limited to the Redis host; with
Headscale it's an ACL rule (`vps-bot → redis:6379` only). Don't give a VPS blanket
access to your home subnet.

## 3. Bring the remote bot online

The [`deploy/remote/`](../deploy/remote) kit is a Compose stack with **no local
Redis** — it points at the hub over the tunnel:

```sh
# on the remote host, after it's joined the mesh:
cd deploy/remote
cp .env.example .env     # REDIS_PASSWORD + this bot's tokens
$EDITOR bot.yaml         # set botnet.redis_addr to the hub's mesh IP, a unique bot: name, its networks
docker compose up -d
docker compose logs -f bot   # look for: "botnet bus connected"
```

Give the remote bot a **unique `bot:` name** and add it to the other bots'
`personality.siblings` so banter/skits include it. Once it logs
`botnet bus connected`, try a `!party` from a home bot — the partyline relays across
the tunnel.

## What can't be scripted here

Standing up the actual mesh, applying the Redis secret to a running cluster, and
provisioning a real VPS are hands-on infra steps — they need the hardware, your
LAN, and real network identities (a Discord token / IRC SASL for the remote bot).
The repo gives you the **runbook and the copy-paste artifacts**; the provisioning
is yours.

## Authenticating bots to each other

The bus is Redis pub/sub, and consumers act on what arrives — including admin
grants. Anyone able to publish to the channel could otherwise make themselves an
owner on every bot, which matters much more once the bus leaves a single trusted
host.

Set `botnet.secret_env` to an env var holding a shared secret, with the **same
value on every sibling bot**:

```yaml
botnet:
  secret_env: "BOTNET_SECRET"
```

Each event is then signed (HMAC-SHA256) and stamped; receivers drop anything that
fails to verify or that has drifted more than five minutes from their clock, which
bounds how long a captured event is worth replaying. Rejected events are counted
so a mismatched secret shows up as a number rather than silence.

Rollout is safe one bot at a time: with no secret configured the bus behaves
exactly as before, so you can deploy the code everywhere first and turn the secret
on afterwards.

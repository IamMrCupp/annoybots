# Changelog

This file is maintained by [release-please](https://github.com/googleapis/release-please)
from Conventional Commit messages. The entry below seeds the initial release;
subsequent releases are appended automatically.

## 0.1.0 (Unreleased)

### Features

* Multi-network annoyance engine: keyword/regex triggers, random ambient
  interjections, and an order-N Markov "brain" that learns from chatter and
  persists across restarts.
* Quote packs loaded from drop-in `.txt` files (classic, Rick & Morty, South
  Park, Futurama, Snuff Box, Tim and Eric, Squidbillies, Orgazmo), surfaced
  randomly and via `!quote`/`!packs` (and `/quote`, `/packs`, `/annoy`,
  `/source` on Discord).
* Multi-network transport: IRC and Twitch (CAP/oauth + rate limiting) plus a
  Discord gateway transport, routed through a shared engine.
* Inter-bot "botnet" over Redis pub/sub: capped sibling banter and coordinated
  multi-bot skits, with loop protection.
* Chat admin console (DM-only): identity-based auth (services account / Discord
  ID / Twitch login) with a password `!login` fallback, channel ops
  (`!join`/`!part`/`!invite`), puppeting (`!say`/`!act`), runtime quote and
  admin management synced over the bus, and `!reload`.
* GitOps deploy: Flux Kustomizations with SOPS-decrypted secrets, ConfigMap
  content for live `!reload`, and release-please + semver image automation.

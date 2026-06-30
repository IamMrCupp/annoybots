# annoybots docs

- **[commands.md](commands.md)** — every public, account, and admin command.
- **[idlerpg.md](idlerpg.md)** — the IdleRPG game: leveling, D&D characters,
  monsters & bosses, loot & enchanting, towns, quests, companions, duels, titles &
  feats, the web dashboard, and Dungeon-Master controls. Start here, or type
  `!rpg help`.
- **[accounts.md](accounts.md)** — link your identities across networks into one
  character.
- **[plugins.md](plugins.md)** — eggdrop-style Lua scripting: add `!commands` with
  a `.lua` file.
- **[federation.md](federation.md)** — run bots on other hosts that join the same
  botnet over a private mesh.

For configuration, the heavily-commented [`configs/echo.yaml`](../configs/echo.yaml)
is the reference — it exercises every feature and all three platforms. For running
it, see the main [README](../README.md) (Kubernetes) or
[`deploy/compose`](../deploy/compose) (Docker Compose, no k8s needed).

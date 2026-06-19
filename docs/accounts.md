# Accounts & cross-network identity

The same person often shows up under different names on different networks — a
nick on IRC, a bouncer nick on another, a Discord handle. Accounts let you tie
those together so features that track a person (IdleRPG, and more later) see **one
identity**, not three.

This is optional. Unlinked, you're tracked per-network — your IRC self and your
Discord self are separate. Linked, they're one.

## How identity is resolved

A sender resolves to a canonical key:

- If the network gives a **verified account** (an IRC services account via SASL or
  the account-tag, a Discord user ID, a Twitch login), that's the anchor.
- Otherwise it's `network|nick` — your nick on that network.

If that identity has been linked to an account, the account name wins. So once
you've linked, every linked identity points at the same character.

## Commands (DM the bot)

| Command | What it does |
|---|---|
| `!register <name> <password>` | Create an account named `<name>`, bound to your current identity. Password is stored as a bcrypt hash. |
| `!link <name> <password>` | Link your *current* identity to an existing account (run it from each network/nick you want joined). |
| `!whoami` | Show which account your current identity resolves to. |
| `!unlink` | Detach your current identity from its account. |

A typical setup: `!register me hunter2` from your IRC nick, then `!link me hunter2`
from Discord (and from any other nick/network). Now all of them are the one
character — your IdleRPG hero levels whether you idle from IRC or Discord.

## Where it's stored

Accounts live in the shared state store, so they persist and are visible to every
bot **only when Redis is on** (`botnet.enabled: true`). With state in-memory,
links reset on restart.

# Commands

Every command works the same on IRC, Twitch, and Discord. On Discord a few are
also exposed as slash commands (noted below); the `!`-prefixed forms work
everywhere.

Most public commands write to the shared state store. That state is **persistent
and shared across bots only when Redis is on** (`botnet.enabled: true`); without
it, it's in-memory and resets on restart.

## Public — in a channel

### Quotes

| Command | What it does |
|---|---|
| `!quote [pack]` | Random quote, optionally from a named pack. Discord: `/quote`. |
| `!packs` | List available quote packs. Discord: `/packs`. |

### Games

| Command | What it does |
|---|---|
| `name++` / `name--` | Bump someone's karma up or down. No self-karma. |
| `!karma [name]` | Show karma for a name (or yourself). |
| `!top` | The karma leaderboard. |

Karma is **one ledger across every network**: if you've linked your identities
with [accounts](accounts.md), praise on IRC and praise on Discord land on the same
score. Unlinked identities keep their own. Pre-existing per-network karma is folded
into the shared ledger automatically on first start.
| `!roll [NdM]` | Roll dice — `!roll` is `1d6`, `!roll 2d20` rolls two twenty-siders. |
| `!8ball <question>` | A magic-8-ball answer. |

### IdleRPG

Opt in with `!rpg`, then "play" by being present and **quiet**. Full rules in
[idlerpg.md](idlerpg.md).

| Command | What it does |
|---|---|
| `!rpg` | Enroll, or show your character. |
| `!rpg help` (`commands`) | List the commands in-channel; full guide on the dashboard at `/help`. |
| `!rpg status [name]` | A character sheet — yours, or a named player's. |
| `!rpg sheet [name]` | The D&D ability block (STR/DEX/CON/INT/WIS/CHA + modifiers), HP, gold, kills. |
| `!rpg race <human\|elf\|dwarf\|halfling\|half-orc\|gnome\|tiefling>` | Choose your heritage once — bakes ability bonuses into your scores. |
| `!rpg items` (`gear`) | Your equipped items and total power. |
| `!rpg top [kills\|gold\|duels]` | Leaderboard — by level (default), or ranked by kills, gold, or duel wins. |
| `!rpg align <good\|neutral\|evil>` or `<lawful\|neutral\|chaotic> <good\|neutral\|evil>` | Set your alignment on the D&D 9-point grid (affects combat). |
| `!rpg class <fighter\|ranger\|rogue\|cleric\|bard\|wizard>` | Pick a class — its primary ability sharpens your attacks (and feeds HP). |
| `!rpg info` | Realm summary: idlers online, top player, active quest. |
| `!rpg quest` | The active quest's party, objective, and time left. |
| `!rpg travel <town>` | Set off for a town; you walk there over the next ticks. |
| `!rpg town` | Where you are on the map (at a town, travelling, or roaming). |
| `!rpg guild [create\|join <name>\|leave\|deposit <gold>]` | Found or join a guild, pool gold in its vault, and level faster when guildmates idle with you. |
| `!rpg guilds` | The guild leaderboard, by summed member levels. |
| `!rpg guild raid` | Spend the vault to summon a champion only your guild can fight; damage tracked, spoils split. |
| `!rpg guild perk [name]` | List the vault perks, or buy the next level (swiftness, fortune, might). |
| `!rpg dungeon` (`delve`) | Your delve — found by roaming; each tick clears a room, the last holds the dungeon's lord. |
| `!rpg pet` (`companion`) | Your companion and the combat bonus it grants (earned by slaying a boss). |
| `!rpg duel <name>` (`spar`) | Friendly best-of-three spar with a present player — bragging rights only, no stat changes. |
| `!rpg feats [name]` (`achievements`) | One-time achievements you've earned (first kill, first boss, a legendary, 1000 gold, …). |
| `!rpg rest` / `!rpg shop` / `!rpg buy <slot>` / `!rpg revive` | Town services — heal, buy gear (gold), revive — usable while at the matching town. |
| `!rpg buy potion` | Buy a healing draught at a market (gold). |
| `!rpg bless` | At a temple, buy a temporary combat blessing (+attack/+damage in the wilds). |
| `!rpg enchant <slot>` | At a market, spend gold to push an equipped item up one rarity tier. |
| `!rpg quaff` (`drink`) | Drink a healing draught to restore full HP — works **anywhere** (self-rescue when downed in the wild). |
| `!rpg pause` / `!rpg resume` | (admin) Freeze or resume the whole game. |
| `!rpg push <name> <secs>` | (admin) Move a player's clock — negative is toward the next level. |
| `!rpg hog [name]` | (admin) Invoke the Hand of God on a named player or a random one. |
| `!rpg raid` | (admin) Summon a world boss for the realm to fight. |
| `!rpg reset <name>` | (admin) Erase one character. |
| `!rpg reset all yes` | (admin) Wipe the **entire** realm — every character + the active quest. The `yes` is required. |
| `!rpg setlevel <name> <n>` | (admin) Set a character's level. |
| `!rpg gold <name> <amount>` | (admin) Grant or remove gold (negative removes). |

Admin `!rpg` verbs use the same identity authorization as the admin console (op flag).
The web dashboard links each leaderboard name to a per-character page at `/p/<name>`,
and serves the full command reference at `/help` (the same source as `!rpg help`).

### Leave a message

| Command | What it does |
|---|---|
| `!message <nick> <text>` | Leave a note; it's delivered when that nick is next active or rejoins. |

### Channel ops

| Command | What it does |
|---|---|
| `!op` | A recognized admin asks the bots to op them. Whichever bot in the channel currently holds channel operator grants `+o`; the mode change is the acknowledgement. |

`!op` is authorized by the same identity model as the admin console (op flag or
better — verified account or hostmask). Non-admins are ignored silently. Every
bot present sees the command, but only the one(s) actually holding ops act, so a
channel full of bots produces neither a mode war nor duplicate replies. Op-state
awareness comes from channel-keeping's per-channel tracking, which is enabled
automatically wherever `!op` is available.

## Accounts — in a DM

Link your identities across networks so you're one character everywhere (one
IdleRPG hero whether you idle from IRC or Discord). See [accounts.md](accounts.md).

| Command | What it does |
|---|---|
| `!register <name> <password>` | Create an account bound to your current identity. |
| `!link <name> <password>` | Link your current identity to an existing account. |
| `!whoami` | Show the account your current identity resolves to. |
| `!unlink` | Detach your current identity from its account. |

## Admin — in a DM

Admins are matched by **verified identity** (an IRC services/NickServ account, a
Discord user ID, or a Twitch login), never by spoofable nick, and admin commands
are only honored in DMs. Access is tiered by flag — owner > master > op > voice >
friend — and each command needs a minimum flag. Configure admins in the `admin:`
block; send `!help` for the list your flags allow.

| Command | Min flag | What it does |
|---|---|---|
| `!claim <code>` | — | First-run bootstrap: become the owner using the one-time code the bot logs at startup when no admins are configured. Needs a verified identity; spent on first use. |
| `!help` / `!admin` | friend | List commands / show your access. |
| `!networks` | friend | Which networks the bot is connected to (connected/offline). |
| `!party` / `!unparty` | friend | Join/leave the partyline (cross-bot operator chat). |
| `!say <net> <target> <text>` | op | Puppet the bot. The target can be a nick or a service — e.g. `!say <net> NickServ IDENTIFY …` to message NickServ directly. |
| `!act <net> <target> <text>` | op | Puppet a `/me`. |
| `!identify <net> [password]` | master | (Re)authenticate the bot to NickServ. Omit the password to use the network's configured secret — nothing sensitive is typed in chat or written to logs. Pass one explicitly only when the network has none configured. |
| `!addquote <pack> <text>` | op | Add a runtime quote. |
| `!delquote <pack> <text>` | op | Remove a runtime-added quote (file packs are immutable). |
| `!join <net> <#chan>` / `!part <net> <#chan>` | master | Channel ops. |
| `!invite <net> <#chan> <nick>` | master | IRC INVITE (needs ops on `+i` channels). |
| `!admins` | master | List admins. |
| `!reload` | master | Re-read quote packs + skits from disk (no restart). |
| `!addadmin <net\|*> <account>` / `!deladmin …` | owner | Manage admins. |

Quote and admin changes **sync to sibling bots over the botnet bus** and persist
to the data volume, so you only have to DM one bot. Channel control and puppeting
stay local to the bot you DM.

**Password fallback.** If a network has no services (or someone isn't logged in),
set `admin.password_env` and an admin can `!login <password>` in a DM for a
time-limited session (`!logout` to end it). It's keyed by nick — spoofable, so
weaker than identity auth — with a constant-time check and a failed-attempt
throttle. Leave `password_env` unset to disable it.

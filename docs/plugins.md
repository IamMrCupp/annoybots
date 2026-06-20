# Plugins (Lua scripting)

annoybots has an eggdrop-style scripting layer: drop a `.lua` file in the plugins
directory and it can add commands without touching Go or rebuilding. It's the
spiritual successor to eggdrop's TCL `bind`.

Turn it on by pointing `plugins.dir` at a directory of scripts:

```yaml
plugins:
  dir: "/plugins"   # the image bakes example scripts here; mount your own to override
```

Every `*.lua` file in that directory is loaded at startup (sorted by name). A
script that fails to load is logged and skipped — one bad plugin can't take the
bot down.

## Writing a plugin

A script registers **binds** at load time. Two kinds:

- `bind("pub", "!cmd", fn)` — fires when someone types `!cmd` in a channel.
- `bind("msg", "!cmd", fn)` — fires when someone DMs the bot `!cmd`.

The callback receives one event table:

| Field | Meaning |
|---|---|
| `ev.nick` | who sent it |
| `ev.chan` | where (channel name, or the sender's nick for a DM) |
| `ev.network` | which network |
| `ev.text` | the full message text |
| `ev.args` | the words after the command — `ev.args[1]`, `ev.args[2]`, … |

Send output with:

- `reply(text)` — back to wherever the command came from.
- `say(network, channel, text)` — anywhere.

```lua
bind("pub", "!hello", function(ev)
  reply("hi " .. ev.nick .. "!")
end)

bind("pub", "!echo", function(ev)
  if #ev.args >= 1 then reply(table.concat(ev.args, " ")) end
end)
```

A fuller example ships at [`data/plugins/example.lua`](../data/plugins/example.lua).

## Sandbox & limits

- Scripts run in a Lua state with **`base`, `string`, `table`, and `math` only** —
  no `os`, `io`, `package`, or `debug`, and the file-loading builtins (`dofile`,
  `loadfile`, `load`) are removed. A plugin can't read the host or shell out.
- Plugins are still **operator-supplied and trusted** — this is a guardrail
  against mistakes, not a hostile-code sandbox. Only load scripts you'd run yourself.
- One shared Lua state serializes all callbacks, so keep them quick; a slow script
  blocks command dispatch.
- v1 covers command binds (`pub`/`msg`). Event binds (join/part/etc.) are a planned
  follow-up.

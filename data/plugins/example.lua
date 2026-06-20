-- Example annoybots plugin (eggdrop-style binds, in Lua).
--
-- Scripts register command "binds" at load time. A "pub" bind fires when someone
-- types the command in a channel; a "msg" bind fires in a private message. The
-- callback receives an event table:
--   ev.nick     who said it
--   ev.chan     where (channel name, or the nick for a DM)
--   ev.network  which network
--   ev.text     the full message text
--   ev.args     the words after the command (ev.args[1], ev.args[2], ...)
--
-- Respond with reply(text) — it goes back to wherever the command came from.
-- say(network, channel, text) sends anywhere. The os/io libraries are NOT
-- available, so a script can't touch the host filesystem.

bind("pub", "!hello", function(ev)
  reply("hi " .. ev.nick .. "!")
end)

bind("pub", "!flip", function(ev)
  if math.random(2) == 1 then
    reply(ev.nick .. ": heads")
  else
    reply(ev.nick .. ": tails")
  end
end)

bind("pub", "!say", function(ev)
  if #ev.args >= 1 then
    reply(table.concat(ev.args, " "))
  end
end)

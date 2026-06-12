package discord

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// slashCommands mirrors the bot's "!" text commands as native Discord commands.
var slashCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "quote",
		Description: "Drop a random quote, optionally from a specific pack.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "pack",
				Description: "quote pack name (e.g. rickmorty, southpark, classic)",
				Required:    false,
			},
		},
	},
	{
		Name:        "annoy",
		Description: "Emit something annoying (Markov babble or a random interjection).",
	},
	{
		Name:        "source",
		Description: "Who is this bot?",
	},
	{
		Name:        "packs",
		Description: "List the available quote packs.",
	},
}

// registerCommands installs the slash commands. Per-guild registration is
// instant; global registration (no guilds configured) can take up to an hour to
// propagate across Discord.
func (c *Client) registerCommands(s *session) {
	appID := ""
	if s.dg.State != nil && s.dg.State.User != nil {
		appID = s.dg.State.User.ID
	}
	if appID == "" {
		s.log.Warn("cannot register slash commands: application ID unknown")
		return
	}

	targets := s.cfg.Guilds
	if len(targets) == 0 {
		targets = []string{""} // "" => global registration
	}
	for _, guild := range targets {
		if _, err := s.dg.ApplicationCommandBulkOverwrite(appID, guild, slashCommands); err != nil {
			s.log.Warn("slash command registration failed", "guild", guild, "err", err)
			continue
		}
		scope := guild
		if scope == "" {
			scope = "global"
		}
		s.log.Info("slash commands registered", "scope", scope)
	}
}

// onInteraction handles incoming slash commands using the same engine helpers as
// the "!" text commands, so output is identical across platforms.
func (c *Client) onInteraction(s *session, dg *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := i.ApplicationCommandData()

	var content string
	switch data.Name {
	case "quote":
		pack := ""
		if len(data.Options) > 0 {
			pack = data.Options[0].StringValue()
		}
		line, unknown := c.cmd.RandomQuote(pack)
		switch {
		case unknown:
			content = "no such quote pack: " + pack
		case line == "":
			content = "...the void offers no quotes."
		default:
			content = line
		}
	case "annoy":
		content = c.cmd.AnnoyLine()
		if content == "" {
			content = "...I've got nothing. For now."
		}
	case "source":
		content = c.cmd.SourceLine()
	case "packs":
		names := c.cmd.PackNames()
		if len(names) == 0 {
			content = "no quote packs loaded"
		} else {
			content = "quote packs: " + strings.Join(names, ", ")
		}
	default:
		return
	}

	if err := dg.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	}); err != nil {
		s.log.Warn("interaction response failed", "command", data.Name, "err", err)
	}
}

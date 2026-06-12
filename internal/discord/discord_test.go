package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestToMessageChannel(t *testing.T) {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{
		ChannelID: "chan123",
		GuildID:   "guild999",
		Content:   "well hello there",
		Author:    &discordgo.User{ID: "u1", Username: "victim"},
	}}
	got := toMessage("discord-main", m, "arywen")

	if got.Network != "discord-main" || got.Channel != "chan123" {
		t.Fatalf("network/channel wrong: %+v", got)
	}
	if got.Nick != "victim" || got.Text != "well hello there" || got.Self != "arywen" {
		t.Fatalf("fields wrong: %+v", got)
	}
	if got.Private {
		t.Fatal("guild message should not be private")
	}
}

func TestToMessageUsesGuildNick(t *testing.T) {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{
		ChannelID: "c",
		GuildID:   "g",
		Content:   "hi",
		Author:    &discordgo.User{ID: "u1", Username: "victim"},
		Member:    &discordgo.Member{Nick: "TheVictim"},
	}}
	if got := toMessage("d", m, "self"); got.Nick != "TheVictim" {
		t.Fatalf("expected guild nick to win, got %q", got.Nick)
	}
}

func TestToMessageDMIsPrivate(t *testing.T) {
	m := &discordgo.MessageCreate{Message: &discordgo.Message{
		ChannelID: "dm",
		GuildID:   "", // DMs have no guild
		Content:   "psst",
		Author:    &discordgo.User{ID: "u1", Username: "victim"},
	}}
	if got := toMessage("d", m, "self"); !got.Private {
		t.Fatal("DM should be private")
	}
}

func TestChannelAllow(t *testing.T) {
	if channelAllow(nil) != nil {
		t.Fatal("empty list should be nil (allow all)")
	}
	a := channelAllow([]string{"1", "2"})
	if !a["1"] || !a["2"] || a["3"] {
		t.Fatalf("allowlist wrong: %#v", a)
	}
}

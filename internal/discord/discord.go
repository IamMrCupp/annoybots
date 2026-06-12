// Package discord is a transport that connects the shared annoyance engine to
// Discord via the gateway (bwmarrin/discordgo). Discord is not IRC under the
// hood -- it's a WebSocket + REST API -- so it is its own transport, but it
// emits the same engine.Message values and implements engine.Sender, so all the
// triggers, quotes, and Markov behavior come along unchanged.
//
// IMPORTANT: reading message text requires the privileged MESSAGE CONTENT
// intent, which must be enabled for the bot application in the Discord developer
// portal. Without it the bot connects but every message body arrives empty.
package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"

	"github.com/mrcupp/annoybots/internal/config"
	"github.com/mrcupp/annoybots/internal/engine"
)

// Commander is the slice of the engine the slash commands need.
type Commander interface {
	RandomQuote(pack string) (string, bool)
	AnnoyLine() string
	SourceLine() string
	PackNames() []string
}

type session struct {
	cfg       config.Network
	dg        *discordgo.Session
	allow     map[string]bool // channel-ID allowlist; nil means allow all
	closeOnce sync.Once
	log       *slog.Logger
}

// Client is the Discord transport. It may hold several sessions (one per bot
// token / "network").
type Client struct {
	sessions map[string]*session
	order    []string
	handler  func(engine.Message)
	cmd      Commander
	log      *slog.Logger
	wg       sync.WaitGroup
}

// New builds (but does not connect) a Discord client for every discord network.
func New(nets []config.Network, handler func(engine.Message), cmd Commander, log *slog.Logger, getenv func(string) string) (*Client, error) {
	c := &Client{sessions: make(map[string]*session), handler: handler, cmd: cmd, log: log}
	for _, n := range nets {
		dg, err := discordgo.New("Bot " + getenv(n.PasswordEnv))
		if err != nil {
			return nil, fmt.Errorf("discord %q: %w", n.Name, err)
		}
		dg.Identify.Intents = discordgo.IntentGuildMessages |
			discordgo.IntentDirectMessages |
			discordgo.IntentMessageContent
		s := &session{
			cfg:   n,
			dg:    dg,
			allow: channelAllow(n.Channels),
			log:   log.With("network", n.Name),
		}
		c.sessions[n.Name] = s
		c.order = append(c.order, n.Name)
		c.bind(s)
	}
	return c, nil
}

// channelAllow turns a channel-ID list into a set. An empty list means "respond
// in every channel the bot can see" and is represented by nil.
func channelAllow(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func (c *Client) bind(s *session) {
	network := s.cfg.Name
	s.dg.AddHandler(func(dg *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot {
			return
		}
		if dg.State != nil && dg.State.User != nil && m.Author.ID == dg.State.User.ID {
			return // ignore our own messages
		}
		if s.allow != nil && !s.allow[m.ChannelID] {
			return
		}
		self, selfID := "", ""
		if dg.State != nil && dg.State.User != nil {
			self = dg.State.User.Username
			selfID = dg.State.User.ID
		}
		c.handler(toMessage(network, m, self, selfID))
	})
	s.dg.AddHandler(func(dg *discordgo.Session, i *discordgo.InteractionCreate) {
		c.onInteraction(s, dg, i)
	})
}

// toMessage converts a Discord message into the engine's transport-agnostic form.
// Raw mentions ("<@id>") are rewritten to display names so that "@Arywen" reads
// as "Arywen" and fires the name-mention trigger like an IRC highlight. Kept as a
// pure function so it can be unit-tested without a live gateway.
func toMessage(network string, m *discordgo.MessageCreate, self, selfID string) engine.Message {
	nick := ""
	if m.Author != nil {
		nick = m.Author.Username
		if m.Member != nil && m.Member.Nick != "" {
			nick = m.Member.Nick
		}
	}
	return engine.Message{
		Network: network,
		Channel: m.ChannelID,
		Nick:    nick,
		Text:    replaceMentions(m.Content, m.Mentions, selfID, self),
		Private: m.GuildID == "",
		Self:    self,
	}
}

// replaceMentions rewrites "<@id>" and "<@!id>" tokens to the mentioned user's
// display name (the bot's own mention becomes self), leaving other text intact.
func replaceMentions(content string, mentions []*discordgo.User, selfID, self string) string {
	for _, u := range mentions {
		if u == nil {
			continue
		}
		name := u.Username
		if u.ID == selfID && self != "" {
			name = self
		}
		content = strings.ReplaceAll(content, "<@"+u.ID+">", name)
		content = strings.ReplaceAll(content, "<@!"+u.ID+">", name)
	}
	return content
}

// Say sends a normal message to a Discord channel ID.
func (c *Client) Say(network, target, text string) {
	s, ok := c.sessions[network]
	if !ok {
		return
	}
	if _, err := s.dg.ChannelMessageSend(target, text); err != nil {
		s.log.Warn("send failed", "channel", target, "err", err)
	}
}

// Action emulates an IRC "/me" using Discord italics.
func (c *Client) Action(network, target, text string) {
	c.Say(network, target, "_"+text+"_")
}

// Networks returns the names of the Discord networks this client owns.
func (c *Client) Networks() []string { return c.order }

// Run opens every session and registers slash commands. Open is non-blocking.
func (c *Client) Run(ctx context.Context) {
	for _, name := range c.order {
		s := c.sessions[name]
		if err := s.dg.Open(); err != nil {
			s.log.Error("discord open failed", "err", err)
			continue
		}
		s.log.Info("discord connected")
		c.registerCommands(s)
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		<-ctx.Done()
		c.Quit()
	}()
}

// Quit closes all sessions exactly once.
func (c *Client) Quit() {
	for _, name := range c.order {
		s := c.sessions[name]
		s.closeOnce.Do(func() {
			if err := s.dg.Close(); err != nil {
				s.log.Warn("discord close failed", "err", err)
			}
		})
	}
}

// Wait blocks until the close goroutine has finished.
func (c *Client) Wait() { c.wg.Wait() }

// AnyConnected reports whether any session has completed its initial handshake.
func (c *Client) AnyConnected() bool {
	for _, s := range c.sessions {
		if s.dg.DataReady {
			return true
		}
	}
	return false
}

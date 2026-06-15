package engine

import "time"

// Message is a normalized inbound chat line, deliberately independent of any
// IRC library so the engine can be unit-tested and reused across networks
// (real IRC, a private InspIRCd test net, Twitch, etc.).
type Message struct {
	Network string // logical network name, e.g. "libera" or "twitch"
	Channel string // channel where it was said; for PMs, the sender's nick
	Nick    string // who said it
	Text    string // message body
	Private bool   // true if a direct message rather than a channel
	Self    string // the bot's own current nick on this network
	Account string // verified identity (IRC services account, Discord user ID, Twitch login); empty if none
	Ident   string // IRC username/ident from the prefix; empty off-IRC
	Host    string // IRC hostname/cloak from the prefix; empty off-IRC
}

// Sender lets the engine emit lines back out to any network by name.
type Sender interface {
	Say(network, target, text string)
	Action(network, target, text string)
}

// Duration is a time.Duration that unmarshals from a human string like "30s".
type Duration time.Duration

// D returns the underlying time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalYAML parses durations written as strings ("45s", "5m").
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Trigger fires a randomized response when an inbound line matches Pattern.
type Trigger struct {
	Name      string   `yaml:"name"`
	Pattern   string   `yaml:"pattern"`
	Chance    float64  `yaml:"chance"`   // 0..1 probability to fire when matched (0 => always)
	Cooldown  Duration `yaml:"cooldown"` // per-channel cooldown for this trigger
	Action    bool     `yaml:"action"`   // send as a /me action
	Responses []string `yaml:"responses"`
}

// Interjections are random ambient lines dropped into channels to be annoying.
type Interjections struct {
	Enabled   bool     `yaml:"enabled"`
	Chance    float64  `yaml:"chance"`   // per qualifying message
	Cooldown  Duration `yaml:"cooldown"` // per channel
	UseMarkov bool     `yaml:"use_markov"`
	Lines     []string `yaml:"lines"`
}

// MarkovConfig controls the learning/generating "brain".
type MarkovConfig struct {
	Enabled  bool `yaml:"enabled"`
	Learn    bool `yaml:"learn"`
	Order    int  `yaml:"order"`
	MaxWords int  `yaml:"max_words"`
}

// AmbientTimer drives self-initiated ambient chatter. Unlike interjections/quotes
// (which only fire in REACTION to an inbound message), this lets the bot speak
// into a channel ON ITS OWN on a timer — the classic BMotion "the bot sometimes
// just talks" behavior. It only targets channels with recent-but-now-quiet
// activity, never a dead or silent-from-the-start one. Disabled by default.
type AmbientTimer struct {
	Enabled      bool     `yaml:"enabled"`
	Interval     Duration `yaml:"interval"`      // how often the timer rolls (default 60s)
	Chance       float64  `yaml:"chance"`        // 0..1 roll per eligible channel per tick (default 0.3)
	Cooldown     Duration `yaml:"cooldown"`      // per-channel min gap between self-initiated lines (default 5m)
	QuietFor     Duration `yaml:"quiet_for"`     // only interject if the channel has been silent >= this (default 90s)
	ActiveWithin Duration `yaml:"active_within"` // skip channels with no activity in this long (default 30m)
}

// QuotePack is a named collection of canned lines, BMotion-style. Lines may be
// listed inline and/or loaded from a File (one quote per line); the config layer
// merges File contents into Lines so the engine only ever sees Lines.
type QuotePack struct {
	Name  string   `yaml:"name"`
	File  string   `yaml:"file"`
	Lines []string `yaml:"lines"`
}

// Quotes drops preloaded quotes into channels, both randomly and on demand.
type Quotes struct {
	Enabled  bool        `yaml:"enabled"`
	Chance   float64     `yaml:"chance"`   // ambient random-quote chance per message
	Cooldown Duration    `yaml:"cooldown"` // per channel
	Command  bool        `yaml:"command"`  // enable the !quote [pack] command
	Packs    []QuotePack `yaml:"packs"`
}

// Banter is controlled bot-to-bot cross-talk. When a known sibling bot speaks,
// this bot may react -- but every reply is bounded by a per-channel cooldown AND
// a windowed cap, so two bots can never runaway-loop and flood a channel.
type Banter struct {
	Enabled      bool     `yaml:"enabled"`
	Chance       float64  `yaml:"chance"`         // probability of reacting to a sibling line
	Cooldown     Duration `yaml:"cooldown"`       // minimum gap between banter replies per channel
	MaxPerWindow int      `yaml:"max_per_window"` // hard cap on banter replies per window per channel
	Window       Duration `yaml:"window"`         // the rolling window for MaxPerWindow
	Action       bool     `yaml:"action"`         // send as a /me action
	Lines        []string `yaml:"lines"`
}

// Personality is the full behavioral config that distinguishes one bot from
// another. Every bot is the same binary with a different Personality.
type Personality struct {
	Name          string        `yaml:"name"`
	Siblings      []string      `yaml:"siblings"` // other bots' nicks/display names (for banter)
	Triggers      []Trigger     `yaml:"triggers"`
	Interjections Interjections `yaml:"interjections"`
	Quotes        Quotes        `yaml:"quotes"`
	Banter        Banter        `yaml:"banter"`
	Markov        MarkovConfig  `yaml:"markov"`
	AmbientTimer  AmbientTimer  `yaml:"ambient_timer"`
	Commands      bool          `yaml:"commands"`
}

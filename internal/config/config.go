// Package config loads and validates a bot's YAML configuration: its networks,
// personality, brain persistence, and health endpoint. Secrets (server
// passwords, Twitch oauth tokens, NickServ passwords) are never stored in the
// file itself -- the file names an environment variable to read them from, so
// they can live in a Kubernetes Secret.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mrcupp/annoybots/internal/engine"
)

// Config is the top-level bot configuration.
type Config struct {
	Bot         string             `yaml:"bot"`
	Health      Health             `yaml:"health"`
	Brain       Brain              `yaml:"brain"`
	Networks    []Network          `yaml:"networks"`
	Personality engine.Personality `yaml:"personality"`
}

// Health configures the k8s liveness/readiness HTTP server.
type Health struct {
	Addr string `yaml:"addr"`
}

// Brain configures Markov persistence.
type Brain struct {
	Path      string          `yaml:"path"`
	SaveEvery engine.Duration `yaml:"save_every"`
}

// Network describes one server connection (real IRC or Twitch).
type Network struct {
	Name               string   `yaml:"name"`
	Kind               string   `yaml:"kind"` // "irc" (default) or "twitch"
	Server             string   `yaml:"server"`
	TLS                bool     `yaml:"tls"`
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify"`
	Nick               string   `yaml:"nick"`
	User               string   `yaml:"user"`
	RealName           string   `yaml:"realname"`
	PasswordEnv        string   `yaml:"password_env"` // env var holding server password / oauth token
	SASL               bool     `yaml:"sasl"`         // use SASL PLAIN (real networks)
	SASLUser           string   `yaml:"sasl_user"`
	SASLPassEnv        string   `yaml:"sasl_pass_env"`
	Channels           []string `yaml:"channels"`
	Rate               Rate     `yaml:"rate"`
}

// Rate configures the outbound token-bucket limiter for a network.
type Rate struct {
	Burst     int     `yaml:"burst"`
	PerSecond float64 `yaml:"per_second"`
}

// Load reads, defaults, validates, and post-processes a config file. quoteDir,
// if non-empty, is the base directory for resolving relative quote-pack files;
// otherwise files are resolved relative to the config file's directory.
func Load(path, quoteDir string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	base := quoteDir
	if base == "" {
		base = filepath.Dir(path)
	}
	if err := loadQuotePacks(&c.Personality, base); err != nil {
		return nil, err
	}

	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// loadQuotePacks merges any File-backed quote packs into their Lines.
func loadQuotePacks(p *engine.Personality, base string) error {
	for i := range p.Quotes.Packs {
		pack := &p.Quotes.Packs[i]
		if pack.File == "" {
			continue
		}
		fp := pack.File
		if !filepath.IsAbs(fp) {
			fp = filepath.Join(base, fp)
		}
		data, err := os.ReadFile(fp)
		if err != nil {
			return fmt.Errorf("quote pack %q: %w", pack.Name, err)
		}
		pack.Lines = append(pack.Lines, parseLines(data)...)
	}
	return nil
}

// parseLines splits a quote file into trimmed, non-empty, non-comment lines.
func parseLines(data []byte) []string {
	var out []string
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func (c *Config) applyDefaults() {
	if c.Health.Addr == "" {
		c.Health.Addr = ":8080"
	}
	for i := range c.Networks {
		n := &c.Networks[i]
		if n.Kind == "" {
			n.Kind = "irc"
		}
		if n.User == "" {
			n.User = n.Nick
		}
		if n.RealName == "" {
			n.RealName = c.Personality.Name
		}
		if n.Kind == "twitch" {
			n.TLS = true
			if n.Server == "" {
				n.Server = "irc.chat.twitch.tv:6697"
			}
			// Twitch: ~20 messages / 30s for a normal account. Stay under it.
			if n.Rate.Burst == 0 {
				n.Rate.Burst = 18
			}
			if n.Rate.PerSecond == 0 {
				n.Rate.PerSecond = 0.6
			}
		}
		if n.Rate.Burst == 0 {
			n.Rate.Burst = 4
		}
		if n.Rate.PerSecond == 0 {
			n.Rate.PerSecond = 0.5
		}
	}
}

// Validate checks the config for fatal problems.
func (c *Config) Validate() error {
	if len(c.Networks) == 0 {
		return fmt.Errorf("no networks configured")
	}
	seen := make(map[string]bool)
	for _, n := range c.Networks {
		switch {
		case n.Name == "":
			return fmt.Errorf("a network is missing its name")
		case seen[n.Name]:
			return fmt.Errorf("duplicate network name %q", n.Name)
		case n.Server == "":
			return fmt.Errorf("network %q is missing server", n.Name)
		case n.Nick == "":
			return fmt.Errorf("network %q is missing nick", n.Name)
		}
		seen[n.Name] = true

		switch n.Kind {
		case "irc", "twitch":
		default:
			return fmt.Errorf("network %q has invalid kind %q (want irc or twitch)", n.Name, n.Kind)
		}
		if n.Kind == "twitch" && n.PasswordEnv == "" {
			return fmt.Errorf("twitch network %q needs password_env (the oauth token)", n.Name)
		}
		if n.SASL && (n.SASLUser == "" || n.SASLPassEnv == "") {
			return fmt.Errorf("network %q has sasl enabled but missing sasl_user/sasl_pass_env", n.Name)
		}
	}
	return nil
}

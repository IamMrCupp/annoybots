package botnet

import "github.com/IamMrCupp/annoybots/internal/engine"

// SkitStep is one line of a scripted multi-bot exchange.
type SkitStep struct {
	Bot    string          `yaml:"bot"`    // which bot delivers this line
	Line   string          `yaml:"line"`   // what it says
	Delay  engine.Duration `yaml:"delay"`  // pause before delivering, for comic timing
	Action bool            `yaml:"action"` // deliver as a /me action
}

// Skit is a named, ordered sequence of lines performed by two or more bots in
// lockstep, coordinated over the bus.
type Skit struct {
	Name     string          `yaml:"name"`
	Chance   float64         `yaml:"chance"`   // auto-start chance on a human message (lead bot only)
	Cooldown engine.Duration `yaml:"cooldown"` // per-channel cooldown between runs
	Steps    []SkitStep      `yaml:"steps"`
}

// Lead returns the bot that owns the first step (the only bot allowed to start
// the skit, which prevents both bots from initiating the same skit at once).
func (s Skit) Lead() string {
	if len(s.Steps) == 0 {
		return ""
	}
	return s.Steps[0].Bot
}

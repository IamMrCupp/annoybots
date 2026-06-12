package engine

import "strings"

// PackNames returns the configured quote-pack names, in order. Used by the
// !packs text command and the Discord /packs slash command so users can discover
// what they can pass to !quote / /quote.
func (e *Engine) PackNames() []string {
	names := make([]string, 0, len(e.p.Quotes.Packs))
	for _, p := range e.p.Quotes.Packs {
		names = append(names, p.Name)
	}
	return names
}

// allQuotes returns every quote across all packs.
func (e *Engine) allQuotes() []string {
	var out []string
	for _, p := range e.p.Quotes.Packs {
		out = append(out, p.Lines...)
	}
	return out
}

// quotesFromPack returns the lines of a pack by case-insensitive name, or nil.
func (e *Engine) quotesFromPack(name string) []string {
	for _, p := range e.p.Quotes.Packs {
		if strings.EqualFold(p.Name, name) {
			return p.Lines
		}
	}
	return nil
}

// maybeQuote randomly emits an ambient quote. Returns true if it emitted.
func (e *Engine) maybeQuote(msg Message, out Sender) bool {
	q := e.p.Quotes
	if !q.Enabled || msg.Private {
		return false
	}
	if !e.roll(q.Chance) {
		return false
	}
	key := "quote:" + msg.Network + ":" + msg.Channel
	if q.Cooldown.D() > 0 && !e.cool.Use(key, q.Cooldown.D()) {
		return false
	}
	line := e.render(e.pick(e.allQuotes()), msg, nil)
	if line == "" {
		return false
	}
	out.Say(msg.Network, msg.Channel, line)
	return true
}

// handlePacksCommand lists the available quote packs.
func (e *Engine) handlePacksCommand(msg Message, out Sender) bool {
	if !e.p.Quotes.Command {
		return false
	}
	names := e.PackNames()
	if len(names) == 0 {
		out.Say(msg.Network, msg.Channel, "no quote packs loaded")
		return true
	}
	out.Say(msg.Network, msg.Channel, "quote packs: "+strings.Join(names, ", ")+" — try !quote <name>")
	return true
}

// handleQuoteCommand implements "!quote" and "!quote <pack>".
func (e *Engine) handleQuoteCommand(msg Message, out Sender) bool {
	if !e.p.Quotes.Command {
		return false
	}
	fields := strings.Fields(msg.Text)
	pack := ""
	if len(fields) > 1 {
		pack = fields[1]
	}
	line, unknown := e.RandomQuote(pack)
	if unknown {
		out.Say(msg.Network, msg.Channel, "no such quote pack: "+pack)
		return true
	}
	if line := e.render(line, msg, nil); line != "" {
		out.Say(msg.Network, msg.Channel, line)
	}
	return true
}

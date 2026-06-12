package engine

import "strings"

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

// handleQuoteCommand implements "!quote" and "!quote <pack>".
func (e *Engine) handleQuoteCommand(msg Message, out Sender) bool {
	if !e.p.Quotes.Command {
		return false
	}
	fields := strings.Fields(msg.Text)
	var pool []string
	if len(fields) > 1 {
		if pool = e.quotesFromPack(fields[1]); pool == nil {
			out.Say(msg.Network, msg.Channel, "no such quote pack: "+fields[1])
			return true
		}
	} else {
		pool = e.allQuotes()
	}
	if line := e.render(e.pick(pool), msg, nil); line != "" {
		out.Say(msg.Network, msg.Channel, line)
	}
	return true
}

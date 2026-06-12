package engine

import "strings"

// configHasPack reports whether name matches a file-backed (config) pack.
func (e *Engine) configHasPack(name string) bool {
	for _, p := range e.p.Quotes.Packs {
		if strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

// PackNames returns every quote-pack name (file packs plus runtime-added packs),
// in a stable order. Used by !packs and the Discord /packs slash command.
func (e *Engine) PackNames() []string {
	names := make([]string, 0, len(e.p.Quotes.Packs))
	for _, p := range e.p.Quotes.Packs {
		names = append(names, p.Name)
	}
	e.qmu.RLock()
	names = append(names, e.customNames...)
	e.qmu.RUnlock()
	return names
}

// allQuotes returns every quote across all packs (file + runtime).
func (e *Engine) allQuotes() []string {
	var out []string
	for _, p := range e.p.Quotes.Packs {
		out = append(out, p.Lines...)
	}
	e.qmu.RLock()
	for _, lines := range e.custom {
		out = append(out, lines...)
	}
	e.qmu.RUnlock()
	return out
}

// quotesFromPack returns the lines of a pack by case-insensitive name (merging
// file lines and runtime-added lines), or nil if the pack is unknown.
func (e *Engine) quotesFromPack(name string) []string {
	var out []string
	for _, p := range e.p.Quotes.Packs {
		if strings.EqualFold(p.Name, name) {
			out = append(out, p.Lines...)
		}
	}
	e.qmu.RLock()
	out = append(out, e.custom[strings.ToLower(name)]...)
	e.qmu.RUnlock()
	if len(out) == 0 {
		return nil
	}
	return out
}

// AddQuote appends a line to a pack at runtime, creating the pack if needed.
// Returns false if the line is empty or already present in the runtime store.
func (e *Engine) AddQuote(pack, line string) bool {
	line = strings.TrimSpace(line)
	if pack == "" || line == "" {
		return false
	}
	key := strings.ToLower(pack)
	e.qmu.Lock()
	defer e.qmu.Unlock()
	for _, existing := range e.custom[key] {
		if existing == line {
			return false
		}
	}
	if _, seen := e.custom[key]; !seen && !e.configHasPack(pack) {
		e.customNames = append(e.customNames, pack) // a brand-new runtime pack
	}
	e.custom[key] = append(e.custom[key], line)
	return true
}

// DelQuote removes a runtime-added line from a pack. It cannot remove lines that
// came from a file pack. Returns true if a line was removed.
func (e *Engine) DelQuote(pack, line string) bool {
	line = strings.TrimSpace(line)
	key := strings.ToLower(pack)
	e.qmu.Lock()
	defer e.qmu.Unlock()
	lines := e.custom[key]
	for i, existing := range lines {
		if existing == line {
			e.custom[key] = append(lines[:i:i], lines[i+1:]...)
			return true
		}
	}
	return false
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

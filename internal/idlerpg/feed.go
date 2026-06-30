package idlerpg

import (
	"context"
	"encoding/json"
)

// The activity feed is a capped, newest-first log of the realm's dramatic
// moments — level-ups, monster and boss kills, deaths, duels, feats, godsends.
// The bots write it as those events fire; the web dashboard reads it to show a
// live "what's happening" panel, giving the game a memory beyond ephemeral chat.

const (
	feedKey = "rpg:feed"
	feedCap = 150 // most recent events retained
)

// FeedEvent is one entry in the activity log.
type FeedEvent struct {
	Ts   int64  `json:"ts"`   // unix seconds when it happened
	Text string `json:"text"` // the announcement, exactly as it appeared in channel
}

// record appends an event to the capped feed. Best-effort — a feed hiccup must
// never disrupt the game.
func (m *Manager) record(text string) {
	blob, err := json.Marshal(FeedEvent{Ts: m.now().Unix(), Text: text})
	if err != nil {
		return
	}
	_ = m.store.ListPush(context.Background(), feedKey, string(blob), feedCap)
}

// drama announces a dramatic event in-channel AND records it to the activity
// feed. Use it in place of out.Say for highlight-reel moments; keep plain Say for
// command replies and routine chatter that shouldn't clutter the feed.
func (m *Manager) drama(network, channel, text string) {
	m.out.Say(network, channel, text)
	m.record(text)
}

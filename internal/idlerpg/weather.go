package idlerpg

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/IamMrCupp/annoybots/internal/state"
)

// Weather is ambient and per-biome — the coast turns stormy, the peaks snowbound,
// the marsh foggy — rotating every so often. It's lighter than a realm-wide world
// event: a mild thumb on the scale wherever you happen to be standing.
//
//	fog   — harder to land blows there (-2 attack)
//	storm — the foes there strike more surely (+2 to their attack)
//	rain  — the going is slow (travel step reduced)
//	snow  — slower still
//	clear — nothing at all
const (
	weatherRotate = 1200 // seconds between rotations
	fogAtkPenalty = 2
	stormAtkBonus = 2
	rainSlow      = 4 // travel step reduction
	snowSlow      = 7
)

func weatherKey() string { return "rpg:weather" }

// weatherState is the realm's current sky, per biome.
type weatherState struct {
	Biomes   map[string]string `json:"biomes"`   // biome -> weather kind
	Deadline int64             `json:"deadline"` // unix seconds until it rotates
}

// biomeWeather constrains what each biome can get — snow in the peaks, storms at sea.
var biomeWeather = map[string][]string{
	"coast":    {"clear", "rain", "storm", "fog"},
	"mountain": {"clear", "snow", "storm", "fog"},
	"forest":   {"clear", "rain", "fog"},
	"swamp":    {"clear", "rain", "fog", "storm"},
	"plains":   {"clear", "rain", "storm"},
}

// WeatherView is the dashboard's read-only view of the sky.
type WeatherView struct {
	Biome string
	Kind  string
}

// ReadWeather returns the current per-biome weather, sorted by biome.
func ReadWeather(ctx context.Context, store state.Store) ([]WeatherView, error) {
	blob, err := store.GetStr(ctx, weatherKey())
	if err != nil || blob == "" {
		return nil, err
	}
	var w weatherState
	if json.Unmarshal([]byte(blob), &w) != nil {
		return nil, nil
	}
	out := make([]WeatherView, 0, len(w.Biomes))
	for b, k := range w.Biomes {
		out = append(out, WeatherView{Biome: b, Kind: k})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Biome < out[j].Biome })
	return out, nil
}

func (m *Manager) loadWeather(ctx context.Context) {
	blob, err := m.store.GetStr(ctx, weatherKey())
	if err != nil || blob == "" {
		return
	}
	var w weatherState
	if json.Unmarshal([]byte(blob), &w) != nil {
		return
	}
	m.wmu.Lock()
	m.weather = &w
	m.wmu.Unlock()
}

func (m *Manager) saveWeather(ctx context.Context, w *weatherState) {
	if blob, err := json.Marshal(w); err == nil {
		_ = m.store.SetStr(ctx, weatherKey(), string(blob))
	}
}

// weatherAt reports the current weather in a biome ("clear" when unknown).
func (m *Manager) weatherAt(biome string) string {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	if m.weather == nil {
		return "clear"
	}
	if k, ok := m.weather.Biomes[biome]; ok {
		return k
	}
	return "clear"
}

// weatherTick rolls a fresh sky for every biome when the current one has run its
// course (and seeds the very first one).
func (m *Manager) weatherTick(ctx context.Context) {
	m.wmu.Lock()
	w := m.weather
	m.wmu.Unlock()
	if w != nil && m.now().Unix() < w.Deadline {
		return
	}
	next := &weatherState{Biomes: map[string]string{}, Deadline: m.now().Unix() + weatherRotate}
	for biome, kinds := range biomeWeather {
		next.Biomes[biome] = kinds[m.roll(len(kinds))]
	}
	m.wmu.Lock()
	m.weather = next
	m.wmu.Unlock()
	m.saveWeather(ctx, next)
}

// travelSlow returns how much a biome's weather shortens a travel step.
func travelSlow(kind string) int {
	switch kind {
	case "rain":
		return rainSlow
	case "snow":
		return snowSlow
	}
	return 0
}

// weatherStatus answers !rpg weather — the sky over every biome.
func (m *Manager) weatherStatus() string {
	m.wmu.Lock()
	w := m.weather
	m.wmu.Unlock()
	if w == nil || len(w.Biomes) == 0 {
		return "the sky is clear everywhere."
	}
	biomes := make([]string, 0, len(w.Biomes))
	for b := range w.Biomes {
		biomes = append(biomes, b)
	}
	sort.Strings(biomes)
	parts := make([]string, len(biomes))
	for i, b := range biomes {
		parts[i] = fmt.Sprintf("%s %s: %s", weatherIcon(w.Biomes[b]), b, w.Biomes[b])
	}
	return "🌤 the sky — " + strings.Join(parts, " · ")
}

func weatherIcon(kind string) string {
	switch kind {
	case "rain":
		return "🌧"
	case "storm":
		return "⛈"
	case "fog":
		return "🌫"
	case "snow":
		return "❄️"
	}
	return "☀️"
}

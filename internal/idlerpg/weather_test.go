package idlerpg

import (
	"context"
	"testing"
	"time"
)

func TestWeatherRotatesAndPersists(t *testing.T) {
	m, _, st := newMgr()
	ctx := context.Background()
	base := time.Unix(1000, 0)
	m.now = func() time.Time { return base }

	m.weatherTick(ctx) // seeds the first sky
	got, err := ReadWeather(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(biomeWeather) {
		t.Fatalf("every biome should have weather, got %d", len(got))
	}
	for _, w := range got {
		allowed := biomeWeather[w.Biome]
		ok := false
		for _, k := range allowed {
			if k == w.Kind {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("biome %q got weather %q not in its allowed set %v", w.Biome, w.Kind, allowed)
		}
	}
	// before the deadline it doesn't re-roll
	first := m.weatherAt("coast")
	m.weatherTick(ctx)
	if m.weatherAt("coast") != first {
		t.Fatal("weather should not rotate before its deadline")
	}
	// after the deadline it re-rolls (kind may repeat, but the deadline must advance)
	m.now = func() time.Time { return base.Add(2 * time.Hour) }
	m.weatherTick(ctx)
	m.wmu.Lock()
	dl := m.weather.Deadline
	m.wmu.Unlock()
	if dl <= base.Unix()+weatherRotate {
		t.Fatal("rotating should push the deadline forward")
	}
}

func TestTravelSlow(t *testing.T) {
	if travelSlow("clear") != 0 || travelSlow("rain") != rainSlow || travelSlow("snow") != snowSlow {
		t.Fatal("travelSlow wrong")
	}
}

func TestWeatherStatus(t *testing.T) {
	m, _, _ := newMgr()
	if got := m.weatherStatus(); got != "the sky is clear everywhere." {
		t.Fatalf("no weather yet → %q", got)
	}
	m.weatherTick(context.Background())
	if got := m.weatherStatus(); got == "" || !contains(got, "the sky") || !contains(got, "coast") {
		t.Fatalf("weather status should list biomes, got %q", got)
	}
}

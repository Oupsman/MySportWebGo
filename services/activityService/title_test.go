package activityService

import (
	"MySportWeb/internal/pkg/types"
	"testing"
	"time"
)

func TestTitle(t *testing.T) {
	paris := UserLocation("Europe/Paris")
	bastille := &types.GpsPoint{Lat: 48.8532, Lon: 2.3692}
	sea := &types.GpsPoint{Lat: 45, Lon: -5}
	for name, c := range map[string]struct {
		sport string
		date  time.Time
		loc   *time.Location
		start *types.GpsPoint
		want  string
	}{
		// 11:30 UTC is 13:30 in Paris in summer: lunch, not morning
		"local time":        {"cycling", time.Date(2026, 7, 1, 11, 30, 0, 0, time.UTC), paris, bastille, "Paris lunch ride"},
		"winter offset":     {"running", time.Date(2026, 1, 15, 17, 30, 0, 0, time.UTC), paris, bastille, "Paris evening run"},
		"midnight is night": {"cycling", time.Date(2026, 3, 1, 23, 30, 0, 0, time.UTC), paris, bastille, "Paris night ride"},
		"1 am is morning":   {"cycling", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), paris, bastille, "Paris morning ride"},
		"afternoon":         {"walking", time.Date(2026, 3, 1, 14, 0, 0, 0, paris), paris, bastille, "Paris afternoon walk"},
		"no city nearby":    {"cycling", time.Date(2026, 3, 1, 8, 0, 0, 0, paris), paris, sea, "Morning ride"},
		"no GPS":            {"swimming", time.Date(2026, 3, 1, 8, 0, 0, 0, paris), paris, nil, "Morning swim"},
		"other sport":       {"open_water", time.Date(2026, 3, 1, 22, 0, 0, 0, paris), paris, nil, "Night open water"},
		"unknown timezone":  {"hiking", time.Date(2026, 3, 1, 11, 30, 0, 0, time.UTC), UserLocation("Mars/Olympus"), nil, "Morning hike"},
	} {
		if got := Title(c.sport, c.date, c.loc, c.start); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

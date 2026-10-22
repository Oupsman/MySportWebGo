package cities

import (
	"MySportWeb/internal/pkg/types"
	"testing"
)

func TestNearest(t *testing.T) {
	for name, c := range map[string]struct {
		point types.GpsPoint
		want  string
	}{
		"Paris center":                {types.GpsPoint{Lat: 48.8566, Lon: 2.3522}, "Paris"},
		"Paris, near the Salpêtrière": {types.GpsPoint{Lat: 48.8380, Lon: 2.3640}, "Paris"},
		"Paris, Bastille":             {types.GpsPoint{Lat: 48.8532, Lon: 2.3692}, "Paris"},
		// on the edge of a city, the center of its neighbour can be nearer than the city's districts
		"Paris, Porte de Versailles": {types.GpsPoint{Lat: 48.8320, Lon: 2.2870}, "Vanves"},
		"Issy-les-Moulineaux center": {types.GpsPoint{Lat: 48.8240, Lon: 2.2700}, "Issy-les-Moulineaux"},
		"Lyon, Guillotière":          {types.GpsPoint{Lat: 45.7550, Lon: 4.8430}, "Lyon"},
		"Marseille, Vieux-Port":      {types.GpsPoint{Lat: 43.2950, Lon: 5.3740}, "Marseille"},
		"Bay of Biscay, no city":     {types.GpsPoint{Lat: 45.0000, Lon: -5.0000}, ""},
		"Orléans center":             {types.GpsPoint{Lat: 47.9030, Lon: 1.9090}, "Orléans"},
	} {
		if got := Nearest(c.point); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

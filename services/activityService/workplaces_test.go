package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"testing"
)

func TestInWorkplace(t *testing.T) {
	office := models.Workplace{Lat: 45.0, Lon: 5.0, Diameter: 200}
	// 0.0009° of latitude is about 100 m
	for _, test := range []struct {
		name string
		p    *types.GpsPoint
		want bool
	}{
		{"center", &types.GpsPoint{Lat: 45, Lon: 5}, true},
		{"90 m away", &types.GpsPoint{Lat: 45.00081, Lon: 5}, true},
		{"110 m away", &types.GpsPoint{Lat: 45.00099, Lon: 5}, false},
		{"no position", nil, false},
		{"null island", &types.GpsPoint{}, false},
	} {
		if got := InWorkplace(test.p, office); got != test.want {
			t.Errorf("%s: %v, want %v", test.name, got, test.want)
		}
	}
	home := &types.GpsPoint{Lat: 45.1, Lon: 5}
	work := &types.GpsPoint{Lat: 45, Lon: 5}
	workplaces := []models.Workplace{{Lat: 46, Lon: 6, Diameter: 500}, office}
	if !startsOrEndsAt(home, work, workplaces) || !startsOrEndsAt(work, home, workplaces) {
		t.Error("a ride from or to the office isn't a commute")
	}
	if startsOrEndsAt(home, home, workplaces) || startsOrEndsAt(nil, nil, workplaces) || startsOrEndsAt(work, work, nil) {
		t.Error("a commute without a workplace")
	}
}

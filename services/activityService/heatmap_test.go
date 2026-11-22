package activityService

import (
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"math"
	"testing"
)

// north returns a straight trace going north, meters long, a point every 100 m
func north(meters float64) []types.GpsPoint {
	var points []types.GpsPoint
	for d := 0.0; d <= meters; d += 100 {
		points = append(points, types.GpsPoint{Lat: 45 + d/111195, Lon: 6})
	}
	return points
}

func length(points []types.GpsPoint) float64 {
	total := 0.0
	for i := 1; i < len(points); i++ {
		total += utils.Haversine(points[i-1], points[i]) * 1000
	}
	return total
}

func TestHideEnds(t *testing.T) {
	trace := north(2000)
	kept := HideEnds(trace, 500)
	if math.Abs(length(kept)-1000) > 2 {
		t.Errorf("kept %.1f m, want 1000", length(kept))
	}
	if d := utils.Haversine(trace[0], kept[0]) * 1000; math.Abs(d-500) > 2 {
		t.Errorf("starts %.1f m after the start", d)
	}
	if d := utils.Haversine(trace[len(trace)-1], kept[len(kept)-1]) * 1000; math.Abs(d-500) > 2 {
		t.Errorf("ends %.1f m before the end", d)
	}
	// a limit between two points: cut where the trace crosses it
	if kept := HideEnds(trace, 450); math.Abs(length(kept)-1100) > 2 {
		t.Errorf("450 m: kept %.1f m", length(kept))
	}
	if kept := HideEnds(north(900), 500); kept != nil {
		t.Errorf("shorter than twice the distance: %v", kept)
	}
	if kept := HideEnds(trace, 0); len(kept) != len(trace) {
		t.Error("nothing to hide: changed")
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"math"
	"testing"
)

// offsetMeters moves a point by dx (east) and dy (north) meters
func offsetMeters(p types.GpsPoint, dx, dy float64) types.GpsPoint {
	return types.GpsPoint{
		Lat: p.Lat + dy/earthRadiusMeters*180/math.Pi,
		Lon: p.Lon + dx/(earthRadiusMeters*math.Cos(p.Lat*math.Pi/180))*180/math.Pi,
	}
}

var origin = types.GpsPoint{Lat: 48.85, Lon: 2.35}

// buildTrace samples a polyline (given in meters) every meter, with an optional lateral noise
func buildTrace(corners [][2]float64, noise func(i int) float64) []types.GpsPoint {
	var trace []types.GpsPoint
	i := 0
	for c := 0; c < len(corners)-1; c++ {
		a, b := corners[c], corners[c+1]
		length := math.Hypot(b[0]-a[0], b[1]-a[1])
		for d := 0.0; d < length; d++ {
			t := d / length
			n := 0.0
			if noise != nil {
				n = noise(i)
			}
			trace = append(trace, offsetMeters(origin, a[0]+t*(b[0]-a[0])+n, a[1]+t*(b[1]-a[1])+n))
			i++
		}
	}
	last := corners[len(corners)-1]
	return append(trace, offsetMeters(origin, last[0], last[1]))
}

var loop = [][2]float64{{0, 0}, {2000, 0}, {2000, 1500}, {0, 1500}, {0, 0}}

func TestDouglasPeuckerKeepsCorners(t *testing.T) {
	trace := buildTrace(loop, nil)
	simplified := DouglasPeucker(trace, SimplifyEpsilon)
	if len(simplified) != len(loop) {
		t.Fatalf("expected %d points, got %d", len(loop), len(simplified))
	}
}

func TestDouglasPeuckerKeepsOutAndBackTip(t *testing.T) {
	// the tip is on the line between start and end: a point-to-line distance would drop it
	trace := buildTrace([][2]float64{{0, 0}, {3000, 0}, {1000, 0}}, nil)
	simplified := DouglasPeucker(trace, SimplifyEpsilon)
	if len(simplified) != 3 {
		t.Fatalf("expected 3 points, got %d", len(simplified))
	}
}

func TestDouglasPeuckerUsesMeters(t *testing.T) {
	// 8 m deviation stays under epsilon whatever the axis
	for _, dir := range [][2]float64{{0, 8}, {8, 0}} {
		trace := buildTrace([][2]float64{{0, 0}, {500 + dir[0], 500 + dir[1]}, {1000, 1000}}, nil)
		if n := len(DouglasPeucker(trace, SimplifyEpsilon)); n != 2 {
			t.Errorf("deviation %v: expected 2 points, got %d", dir, n)
		}
	}
}

func TestSameRouteWithGpsNoise(t *testing.T) {
	a := SimplifyTrace(buildTrace(loop, func(i int) float64 { return 12 * math.Sin(float64(i)/40) }))
	b := SimplifyTrace(buildTrace(loop, func(i int) float64 { return -10 * math.Cos(float64(i)/25) }))
	if s := TraceSimilarity(a, b); s < similarMinCoverage {
		t.Fatalf("expected similar routes, got similarity %.2f", s)
	}
}

func TestReversedLoopIsSimilar(t *testing.T) {
	reversed := make([][2]float64, len(loop))
	for i := range loop {
		reversed[i] = loop[len(loop)-1-i]
	}
	if !TracesSimilar(SimplifyTrace(buildTrace(loop, nil)), SimplifyTrace(buildTrace(reversed, nil))) {
		t.Fatal("expected reversed loop to be similar")
	}
}

func TestDetourIsNotSimilar(t *testing.T) {
	// same start, end and roughly same distance, but one side is 400 m away
	detour := [][2]float64{{0, 0}, {2000, 0}, {2000, 1500}, {1000, 1500}, {1000, 1900}, {0, 1900}, {0, 0}}
	a := SimplifyTrace(buildTrace(loop, nil))
	b := SimplifyTrace(buildTrace(detour, nil))
	if s := TraceSimilarity(a, b); s >= similarMinCoverage {
		t.Fatalf("expected different routes, got similarity %.2f", s)
	}
}

func TestShortcutIsNotSimilar(t *testing.T) {
	// b is a subset of a: a is not covered by b
	a := SimplifyTrace(buildTrace(loop, nil))
	b := SimplifyTrace(buildTrace([][2]float64{{0, 0}, {2000, 0}, {0, 0}}, nil))
	if TracesSimilar(a, b) {
		t.Fatal("expected out-and-back to differ from loop")
	}
}

func TestEndpointsMatch(t *testing.T) {
	near := offsetMeters(origin, 150, 0)
	far := offsetMeters(origin, 300, 0)
	if !EndpointsMatch(&origin, &origin, &near, &near) {
		t.Error("expected 150 m endpoints to match")
	}
	if EndpointsMatch(&origin, &origin, &origin, &far) {
		t.Error("expected 300 m end to not match")
	}
	if EndpointsMatch(nil, &origin, &origin, &origin) {
		t.Error("expected missing endpoint to not match")
	}
}

func TestRouteRequiresSameSport(t *testing.T) {
	trace := SimplifyTrace(buildTrace(loop, nil))
	start, end := trace[0], trace[len(trace)-1]
	activity := func(sport string) models.Activity {
		return models.Activity{Sport: sport, Distance: 7000, StartPosition: &start, EndPosition: &end, SimplifiedGpsPoints: trace}
	}
	runRoute := NewRoute(activity("running"))

	if MatchRoute(activity("cycling"), []models.Route{runRoute}) != nil {
		t.Error("a ride matched a running route")
	}
	if ActivitiesSimilar(activity("running"), activity("cycling")) {
		t.Error("a run is similar to a ride")
	}
	if MatchRoute(activity("running"), []models.Route{runRoute}) == nil {
		t.Error("a run did not match the same running route")
	}
}

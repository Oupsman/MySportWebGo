package activityService

import (
	"math"
	"math/rand"
	"testing"
)

// the series of an activity: a record a second, at speed m/s, the stops recorded or not
type movingSeries struct {
	times, distances []float64
}

func (r *movingSeries) add(seconds int, speed float64) {
	t, d := 0.0, 0.0
	if n := len(r.times); n > 0 {
		t, d = r.times[n-1], r.distances[n-1]
	}
	for i := 1; i <= seconds; i++ {
		r.times = append(r.times, t+float64(i))
		r.distances = append(r.distances, d+speed*float64(i))
	}
}

func TestMovingTime(t *testing.T) {
	// 10 min at 7 m/s, a 5 min stop the device kept recording, 10 min more
	var r movingSeries
	r.times, r.distances = []float64{0}, []float64{0}
	r.add(600, 7)
	r.add(300, 0)
	r.add(600, 7)
	moving, ok := MovingTime("cycling", r.times, r.distances)
	if !ok || math.Abs(moving-1200) > 5 {
		t.Errorf("with a recorded stop: %v %v, want 1200 s", moving, ok)
	}

	// the GPS drifting at the stop: a few tenths of a meter a second, back and forth
	var drift movingSeries
	drift.times, drift.distances = []float64{0}, []float64{0}
	drift.add(600, 7)
	for i := 0; i < 300; i++ {
		last := drift.distances[len(drift.distances)-1]
		drift.times = append(drift.times, drift.times[len(drift.times)-1]+1)
		drift.distances = append(drift.distances, last+rand.Float64()*0.4)
	}
	drift.add(600, 7)
	if moving, _ := MovingTime("cycling", drift.times, drift.distances); math.Abs(moving-1200) > 10 {
		t.Errorf("GPS drift at the stop: %v, want about 1200 s", moving)
	}

	// a tunnel: 60 s without record, 400 m covered; a pause of the device: 120 s, nothing covered
	tunnel := movingSeries{times: []float64{0}, distances: []float64{0}}
	tunnel.add(300, 7)
	tunnel.times = append(tunnel.times, tunnel.times[len(tunnel.times)-1]+60)
	tunnel.distances = append(tunnel.distances, tunnel.distances[len(tunnel.distances)-1]+400)
	tunnel.times = append(tunnel.times, tunnel.times[len(tunnel.times)-1]+120)
	tunnel.distances = append(tunnel.distances, tunnel.distances[len(tunnel.distances)-1])
	tunnel.add(300, 7)
	if moving, _ := MovingTime("cycling", tunnel.times, tunnel.distances); math.Abs(moving-660) > 5 {
		t.Errorf("tunnel and pause: %v, want 660 s", moving)
	}

	// a walk at 0.8 m/s (2.9 km/h) moves, as a ride it stands still
	walk := movingSeries{times: []float64{0}, distances: []float64{0}}
	walk.add(600, 0.8)
	if moving, _ := MovingTime("walking", walk.times, walk.distances); math.Abs(moving-600) > 5 {
		t.Errorf("walk: %v", moving)
	}
	if moving, _ := MovingTime("cycling", walk.times, walk.distances); moving > 5 {
		t.Errorf("a ride at 2.9 km/h: %v, want still", moving)
	}

	// no distance: unknown
	if _, ok := MovingTime("cycling", []float64{0, 10, 20}, []float64{0, 0, 0}); ok {
		t.Error("no distance: known")
	}
	if _, ok := MovingTime("cycling", []float64{0, 10, 20}, []float64{math.NaN(), math.NaN(), math.NaN()}); ok {
		t.Error("NaN distances: known")
	}
	// the moving time is at most the timer's duration
	if moving, speed := movingOf("cycling", nil, nil, 0, 100); moving != 0 || speed != 0 {
		t.Errorf("no series: %v %v", moving, speed)
	}
}

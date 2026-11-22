package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"math"
	"testing"
)

// variedRide is two hours of power changing every second, with a 10 minutes pause after the first hour
func variedRide() ([]float64, []float64) {
	var times, powers []float64
	for s := 0; s < 7200; s++ {
		t := float64(s)
		if s >= 3600 {
			t += 600
		}
		times = append(times, t)
		powers = append(powers, 200+80*math.Sin(float64(s)/37)+40*math.Sin(float64(s)/5))
	}
	return times, powers
}

// TestPowerCurveMeetsBestEfforts: at the durations of the best efforts, the curve is the best efforts
func TestPowerCurveMeetsBestEfforts(t *testing.T) {
	times, powers := variedRide()
	curve := ComputePowerCurve(times, powers)
	s := effortSamples{sport: "cycling", times: times, powers: powers}
	efforts := s.efforts()
	found := 0
	for _, e := range efforts {
		if e.Kind != types.EffortPower {
			continue
		}
		for _, p := range curve {
			if p.Duration == e.Target {
				found++
				if p.Watts != e.Value || p.StartOffset != e.StartOffset {
					t.Errorf("%.0f s: curve %.0f W at %.1f, best effort %.0f W at %.1f",
						e.Target, p.Watts, p.StartOffset, e.Value, e.StartOffset)
				}
			}
		}
	}
	if found != len(PowerDurations) {
		t.Errorf("%d durations in common, want %d", found, len(PowerDurations))
	}
	// the pause counts as 0 W: 2 h fit in the 2 h 10 recorded, not 2 h 30. (The curve isn't always
	// falling: [10 0 10] averages more over 3 s than over any 2 s.)
	last := curve[len(curve)-1]
	if last.Duration != 7200 {
		t.Errorf("longest duration %.0f s, want 7200 (2 h 10 recorded)", last.Duration)
	}
}

func TestPowerCurveShortAndEmpty(t *testing.T) {
	times, powers := powerStream(nil, nil, 0, 300, 100)
	curve := ComputePowerCurve(times, powers)
	if len(curve) == 0 || curve[len(curve)-1].Duration != 90 || curve[0].Watts != 300 {
		t.Errorf("100 s: %v", curve)
	}
	if c := ComputePowerCurve(nil, nil); c == nil || len(c) != 0 {
		t.Errorf("no power: %#v", c)
	}
	// a run gets none, nor an activity without power
	run := rideWithPower(250, 600)
	run.Sport = "running"
	setPowerCurve(&run)
	if run.PowerCurve == nil || len(run.PowerCurve) != 0 {
		t.Errorf("run: %v", run.PowerCurve)
	}
	ride := rideWithPower(250, 600)
	setPowerCurve(&ride)
	if len(ride.PowerCurve) == 0 || ride.PowerCurve[0].Watts != 250 {
		t.Errorf("ride: %v", ride.PowerCurve)
	}
}

func TestFitCriticalPower(t *testing.T) {
	model := func(cp, wPrime float64) []models.PowerCurveBest {
		var curve []models.PowerCurveBest
		for _, d := range PowerCurveDurations {
			curve = append(curve, models.PowerCurveBest{Duration: d, Watts: cp + wPrime/d})
		}
		return curve
	}
	got := FitCriticalPower(model(250, 20000))
	if got == nil || got.CP != 250 || got.WPrime != 20000 || got.R2 != 1 {
		t.Fatalf("model: %+v", got)
	}
	// efforts that weren't all-out: the 5 minutes below the 20
	broken := model(250, 20000)
	for i := range broken {
		if broken[i].Duration == 300 || broken[i].Duration == 360 {
			broken[i].Watts = 150
		}
	}
	if got := FitCriticalPower(broken); got != nil {
		t.Errorf("not all-out: %+v", got)
	}
	// too few points between 3 and 20 minutes
	if got := FitCriticalPower(model(250, 20000)[:14]); got != nil {
		t.Errorf("two points: %+v", got)
	}
}

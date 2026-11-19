package health

import (
	"math"
	"reflect"
	"testing"
	"time"
)

var readinessNow = time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)

// dailyLoads returns a load of score on each of the days before now (now's day included)
func dailyLoads(now time.Time, days int, score float64) []Load {
	var loads []Load
	for i := 0; i < days; i++ {
		loads = append(loads, Load{Date: now.AddDate(0, 0, -i).Add(-time.Hour), Score: score})
	}
	return loads
}

func TestReadinessSteadyLoad(t *testing.T) {
	// a year of the same load: fitness and fatigue both reach it, the form is 0
	r := ComputeReadiness(dailyLoads(readinessNow, 365, 60), nil, readinessNow, 30)
	if len(r.Series) != 30 || r.Series[29].Date != "2026-11-18" || r.Series[0].Date != "2026-10-20" {
		t.Fatalf("series of %d days from %s to %s", len(r.Series), r.Series[0].Date, r.Series[len(r.Series)-1].Date)
	}
	today := r.Today
	if math.Abs(today.Fitness-60) > 1 || today.Fatigue != 60 || math.Abs(today.Form) > 1 {
		t.Errorf("fitness %v fatigue %v form %v, want 60 60 0", today.Fitness, today.Fatigue, today.Form)
	}
	if today.Status != Neutral || today.ACWR == nil || math.Abs(*today.ACWR-1) > 0.02 || len(today.Flags) != 0 {
		t.Errorf("status %s acwr %v flags %v, want neutral 1 none", today.Status, deref(today.ACWR), today.Flags)
	}
}

func TestReadinessHardWeek(t *testing.T) {
	// months at 50, then a week at 150: the fatigue jumps, the form falls, the load rose too fast
	loads := dailyLoads(readinessNow.AddDate(0, 0, -7), 180, 50)
	loads = append(loads, dailyLoads(readinessNow, 7, 150)...)
	today := ComputeReadiness(loads, nil, readinessNow, 14).Today
	if today.Form >= OverreachForm {
		t.Errorf("form %v after a week at 3 times the usual load, want below %v", today.Form, OverreachForm)
	}
	if today.Status != Overreaching || !reflect.DeepEqual(today.Flags, []string{LoadSpikeFlag}) {
		t.Errorf("status %s flags %v, want overreaching and a load spike", today.Status, today.Flags)
	}

	// then a week of rest: fresh again
	rested := ComputeReadiness(loads, nil, readinessNow.AddDate(0, 0, 10), 14).Today
	if rested.Status != Fresh || rested.Form <= FreshForm {
		t.Errorf("after rest: status %s form %v, want fresh", rested.Status, rested.Form)
	}
}

func TestReadinessFormIsTheMorningOne(t *testing.T) {
	// one load today: today's form is the one before it, the fitness and the fatigue include it
	r := ComputeReadiness([]Load{{Date: readinessNow, Score: 100}}, nil, readinessNow, 7)
	today := r.Today
	if today.Form != 0 || today.Load != 100 || today.Fatigue != 13.3 || today.Fitness != 2.4 {
		t.Errorf("load %v fitness %v fatigue %v form %v", today.Load, today.Fitness, today.Fatigue, today.Form)
	}
}

func TestReadinessNotEnoughData(t *testing.T) {
	cases := []struct {
		name  string
		loads []Load
	}{
		{"nothing", nil},
		{"two weeks of history", dailyLoads(readinessNow, 14, 60)},
		{"nothing for two months", dailyLoads(readinessNow.AddDate(0, 0, -60), 100, 60)},
		{"only loads in the future", []Load{{Date: readinessNow.AddDate(0, 0, 1), Score: 60}}},
	}
	for _, c := range cases {
		today := ComputeReadiness(c.loads, nil, readinessNow, 7).Today
		if today.Status != NotEnoughData || len(today.Flags) != 0 {
			t.Errorf("%s: status %s flags %v, want not enough data", c.name, today.Status, today.Flags)
		}
	}
}

func TestReadinessDaysInTheLocation(t *testing.T) {
	// 23:30 UTC on the 17th is the 18th in Paris: today's load there
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, paris)
	r := ComputeReadiness([]Load{{Date: time.Date(2026, 11, 17, 23, 30, 0, 0, time.UTC), Score: 40}}, nil, now, 2)
	if r.Series[1].Date != "2026-11-18" || r.Series[1].Load != 40 || r.Series[0].Load != 0 {
		t.Errorf("series %+v, want the load on the 18th", r.Series)
	}
}

func TestReadinessRestHR(t *testing.T) {
	rests := func(days []int, bpm float64) []RestMeasure {
		var measures []RestMeasure
		for _, d := range days {
			measures = append(measures, RestMeasure{Date: readinessNow.AddDate(0, 0, -d).Add(-12 * time.Hour), BPM: bpm})
		}
		return measures
	}
	baseline := rests([]int{7, 9, 12, 15, 18, 21, 24, 27, 30, 33, 36}, 50) // 11 measures, days 8 to 37
	ignored := rests([]int{37, 50}, 90)                                    // before the baseline
	cases := []struct {
		name     string
		measures []RestMeasure
		delta    *float64
		flagged  bool
	}{
		{"elevated", concat(baseline, ignored, rests([]int{0, 2, 4}, 56)), ptr(6), true},
		{"normal", concat(baseline, rests([]int{0, 1, 2, 3}, 52)), ptr(2), false},
		{"too few recent measures", concat(baseline, rests([]int{0, 2}, 60)), nil, false},
		{"too short a baseline", concat(baseline[:9], rests([]int{0, 2, 4}, 60)), nil, false},
	}
	for _, c := range cases {
		today := ComputeReadiness(nil, c.measures, readinessNow, 7).Today
		if !reflect.DeepEqual(today.RestHRDelta, c.delta) {
			t.Errorf("%s: delta %v, want %v", c.name, deref(today.RestHRDelta), deref(c.delta))
		}
		flagged := len(today.Flags) == 1 && today.Flags[0] == ElevatedRestHRFlag
		if flagged != c.flagged {
			t.Errorf("%s: flags %v", c.name, today.Flags)
		}
	}
	// the averages are given even without enough measures for the delta
	today := ComputeReadiness(nil, rests([]int{0}, 55), readinessNow, 7).Today
	if today.RestHR7d == nil || *today.RestHR7d != 55 || today.RestHR30d != nil {
		t.Errorf("rest 7d %v 30d %v, want 55 and none", deref(today.RestHR7d), deref(today.RestHR30d))
	}
}

func TestReadinessFrom(t *testing.T) {
	if from := ReadinessFrom(readinessNow, 90); !from.Equal(time.Date(2026, 1, 23, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("loads from %s", from)
	}
	if from := RestFrom(readinessNow); !from.Equal(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("resting heart rates from %s", from)
	}
}

func concat(lists ...[]RestMeasure) []RestMeasure {
	var all []RestMeasure
	for _, l := range lists {
		all = append(all, l...)
	}
	return all
}

func ptr(x float64) *float64 { return &x }

func deref(x *float64) interface{} {
	if x == nil {
		return nil
	}
	return *x
}

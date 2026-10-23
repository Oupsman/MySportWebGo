package health

import (
	"math"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)

// series returns one measure every `every` days, the last one today, going from start by step each time
func series(count, every int, start, step float64) []Measure {
	measures := make([]Measure, count)
	for i := range measures {
		measures[i] = Measure{
			Date:   now.AddDate(0, 0, -(count-1-i)*every),
			Weight: start + float64(i)*step,
		}
	}
	return measures
}

func days(date *time.Time) float64 {
	return date.Sub(now).Hours() / 24
}

func TestEstimateStatuses(t *testing.T) {
	for _, test := range []struct {
		name     string
		measures []Measure
		target   float64
		status   string
	}{
		{"no target", series(8, 7, 80, -0.5), 0, NoTarget},
		{"no measure", nil, 75, NotEnoughData},
		{"two measures", series(2, 14, 80, -1), 75, NotEnoughData},
		{"a week of measures", series(5, 1, 80, -0.1), 75, NotEnoughData},
		// the trend is made of the last 60 days only
		{"old measures", []Measure{{now.AddDate(0, -6, 0), 80}, {now.AddDate(0, -5, 0), 79}, {now.AddDate(0, -4, 0), 78}}, 75, NotEnoughData},
		{"reached", series(8, 7, 78, -0.4), 75.4, Reached},
		// 78 to 74.5 while losing: across the target, reached
		{"past the target", series(8, 7, 78, -0.5), 75, Reached},
		// two measures are enough to see it went across
		{"past the target, few measures", series(2, 7, 76, -2), 75, Reached},
		{"losing, target below", series(8, 7, 80, -0.5), 74, OnTrack},
		{"gaining, target above", series(8, 7, 60, 0.3), 65, OnTrack},
		{"losing, target above", series(8, 7, 80, -0.5), 85, WrongDirection},
		{"flat", series(8, 7, 80, 0), 75, WrongDirection},
		// 20 g a week for 10 kg: about ten years
		{"too slow", series(8, 7, 80, -0.02), 70, TooSlow},
	} {
		got := EstimateWeight(test.measures, test.target, now)
		if got.Status != test.status {
			t.Errorf("%s: %s, want %s (%+v)", test.name, got.Status, test.status, got)
		}
		if (got.Date != nil) != (test.status == OnTrack) {
			t.Errorf("%s: date %v with status %s", test.name, got.Date, got.Status)
		}
	}
}

func TestEstimateDate(t *testing.T) {
	// 0.5 kg a week, 76.5 kg today: 74 kg in 5 weeks
	got := EstimateWeight(series(8, 7, 80, -0.5), 74, now)
	if math.Abs(got.PerWeek+0.5) > 1e-9 || math.Abs(got.Current-76.5) > 1e-9 {
		t.Fatalf("trend %+v", got)
	}
	if math.Abs(days(got.Date)-35) > 0.01 {
		t.Errorf("reached in %.2f days, want 35", days(got.Date))
	}
	if got.Measures != 8 {
		t.Errorf("%d measures", got.Measures)
	}
}

func TestEstimateIgnores(t *testing.T) {
	measures := series(8, 7, 80, -0.5)
	// a blood pressure without weight, a measure dated tomorrow (a wrong clock)
	measures = append(measures, Measure{now.AddDate(0, 0, -2), 0}, Measure{now.AddDate(0, 0, 1), 50})
	got := EstimateWeight(measures, 74, now)
	if got.Status != OnTrack || got.Measures != 8 || math.Abs(days(got.Date)-35) > 0.01 {
		t.Errorf("%+v", got)
	}
}

func TestEstimateTrendAlreadyPast(t *testing.T) {
	// a fast loss, then 75.5 this morning: every measure is above the target, the line is at 74.76
	// now, on track, today
	measures := []Measure{
		{now.AddDate(0, 0, -28), 80}, {now.AddDate(0, 0, -21), 78}, {now.AddDate(0, 0, -14), 76.5},
		{now.AddDate(0, 0, -7), 75.4}, {now, 75.5},
	}
	got := EstimateWeight(measures, 75, now)
	if got.Status != OnTrack || got.Date == nil || !got.Date.Equal(now) {
		t.Errorf("%+v", got)
	}
	if got.Current >= 75 {
		t.Fatalf("the trend should be past the target: %+v", got)
	}
}

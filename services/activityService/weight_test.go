package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
	"time"
)

func TestCombineWeights(t *testing.T) {
	cases := []struct {
		rider, equipment float64
		sport            string
		want             float64
	}{
		{72, 9, "cycling", 81},
		{72, 0, "cycling", 72 + DefaultEquipmentWeight}, // a bike without weight
		{0, 0, "cycling", DefaultRiderWeight + DefaultEquipmentWeight},
		{72, 0, "running", 72}, // no default equipment out of the rides
		{72, 1, "running", 73},
		{72, 0, "triathlon", 72 + DefaultEquipmentWeight},
	}
	for _, c := range cases {
		if got := combineWeights(c.rider, c.equipment, c.sport); got != c.want {
			t.Errorf("combineWeights(%v, %v, %s) = %v, want %v", c.rider, c.equipment, c.sport, got, c.want)
		}
	}
}

func TestRiderWeightAt(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 3, d, 8, 0, 0, 0, time.UTC) }
	measures := []models.WeightMeasure{{Date: day(10), Weight: 80}, {Date: day(20), Weight: 78}}
	cases := []struct {
		date time.Time
		want float64
	}{
		{day(5), 80},                    // before the first measure: the first one
		{day(10), 80},                   // the day of a measure
		{day(15), 80},                   // between: the last one before
		{day(20).Add(time.Hour), 78},    // after the last one
		{day(10).Add(-time.Second), 80}, // just before the first one
	}
	for _, c := range cases {
		if got := riderWeightAt(measures, 70, c.date); got != c.want {
			t.Errorf("%v: %v kg, want %v", c.date, got, c.want)
		}
	}
	if got := riderWeightAt(nil, 65, day(1)); got != 65 {
		t.Errorf("without measure: %v kg, want the profile's 65", got)
	}
}

// TestAnalysisWeight: the power estimated without power meter grows with the weight, the activity tells the
// weight it was analysed with
func TestAnalysisWeight(t *testing.T) {
	path := "../../prototypes/" + sameRouteFiles[0] + ".fit"
	analyse := func(kg float64) models.Activity {
		t.Helper()
		weight := func(time.Time, string) (float64, error) { return kg, nil }
		a, err := AnalyzeTrimmed(path, models.Users{}, 0, Analysis{Weight: weight})
		if err != nil {
			t.Skip("FIT file missing in prototypes/:", err)
		}
		return a
	}
	light, heavy := analyse(60), analyse(120)
	if light.TotalWeight != 60 || heavy.TotalWeight != 120 {
		t.Errorf("total weights %d and %d, want 60 and 120", light.TotalWeight, heavy.TotalWeight)
	}
	if light.Sport != "cycling" || light.AvgPower == 0 {
		t.Skipf("%s: a %s without estimated power", path, light.Sport)
	}
	if heavy.AvgPower <= light.AvgPower {
		t.Errorf("average power %d W at 120 kg, not above %d W at 60 kg", heavy.AvgPower, light.AvgPower)
	}
	// without weight: the defaults, not the old 110 kg
	none, err := AnalyzeTrimmed(path, models.Users{}, 0, Analysis{})
	if err != nil {
		t.Fatal(err)
	}
	if want := uint16(DefaultRiderWeight + DefaultEquipmentWeight); none.TotalWeight != want {
		t.Errorf("default total weight %d, want %d", none.TotalWeight, want)
	}
}

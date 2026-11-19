package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"fmt"
	"math"
	"path/filepath"
	"testing"
)

// steady returns a sample per second for seconds seconds at speed m/s
func steady(seconds int, speed float64) ([]float64, []float64) {
	times := make([]float64, seconds+1)
	distances := make([]float64, seconds+1)
	for i := range times {
		times[i] = float64(i)
		distances[i] = float64(i) * speed
	}
	return times, distances
}

func TestFastestOver(t *testing.T) {
	// 4 m/s, then 5 m/s from 500 s: the fastest kilometer takes 200 s, the first one at 5 m/s wins
	times, distances := steady(1000, 4)
	for i := 500; i <= 1000; i++ {
		distances[i] = 2000 + float64(i-500)*5
	}
	seconds, start, found := fastestOver(times, distances, 1000, 10)
	if !found || math.Abs(seconds-200) > 1e-9 || math.Abs(start-500) > 1e-9 {
		t.Errorf("fastest 1000 m: %v s from %v s (%v), want 200 s from 500 s", seconds, start, found)
	}
	// the start falls between two records: interpolated. 4 m/s, 1000 m from 0 to 250 s: any window of
	// 250 s, the first one wins
	seconds, start, found = fastestOver(times[:300], distances[:300], 1000, 10)
	if !found || math.Abs(seconds-250) > 1e-9 || start != 0 {
		t.Errorf("steady 1000 m: %v s from %v s, want 250 s from 0 s", seconds, start)
	}
	// a 1 Hz recording, sampled every 3 s: the start is interpolated, still 250 s
	var sparseT, sparseD []float64
	for i := 0; i < 300; i += 3 {
		sparseT, sparseD = append(sparseT, times[i]), append(sparseD, distances[i]+2) // 2 m offset
	}
	seconds, _, found = fastestOver(sparseT, sparseD, 1000, 10)
	if !found || seconds > 252 || seconds < 249 {
		t.Errorf("sparse 1000 m: %v s, want about 250 s", seconds)
	}
	if _, _, found := fastestOver(times[:100], distances[:100], 1000, 10); found {
		t.Error("an effort longer than the activity")
	}
}

func TestFastestOverGPSJump(t *testing.T) {
	// 3 m/s with a 600 m jump in 1 s at 200 s: without the check, a 1000 m in about 134 s
	times, distances := steady(1000, 3)
	for i := 200; i <= 1000; i++ {
		distances[i] += 600
	}
	seconds, start, found := fastestOver(times, distances, 1000, 10)
	if !found || math.Abs(seconds-1000.0/3) > 1e-6 {
		t.Errorf("1000 m across a GPS jump: %v s from %v s, want %v s", seconds, start, 1000.0/3)
	}
	if start < 200 {
		t.Errorf("the effort spans the jump, starts at %v s", start)
	}
}

func TestDistanceSeries(t *testing.T) {
	times := []float64{0, 1, 2, 2, 3, 4}
	distances := []float64{0, 5, math.NaN(), 12, 8, 20}
	ts, ds := distanceSeries(times, distances)
	// the NaN dropped (its time stamp goes to the next record), 8 raised to the 12 before it
	want := []float64{0, 5, 12, 12, 20}
	wantT := []float64{0, 1, 2, 3, 4}
	if fmt.Sprint(ds) != fmt.Sprint(want) || fmt.Sprint(ts) != fmt.Sprint(wantT) {
		t.Errorf("distance series: %v %v, want %v %v", ts, ds, wantT, want)
	}
}

func TestPowerSeries(t *testing.T) {
	// 200 W at 10 s, 300 W at 12 s, nothing until 30 s (a pause): 0 W after 17 s, a glitch ignored
	times := []float64{10, 12, 13, 30}
	powers := []float64{200, 300, 5000, 100}
	first, perSecond := powerSeries(times, powers)
	if first != 10 || len(perSecond) != 21 {
		t.Fatalf("first %v, %d seconds, want 10 and 21", first, len(perSecond))
	}
	want := map[int]float64{0: 200, 1: 200, 2: 300, 3: 300, 7: 300, 8: 0, 19: 0, 20: 100}
	for s, w := range want {
		if perSecond[s] != w {
			t.Errorf("second %d: %v W, want %v", s, perSecond[s], w)
		}
	}
	if _, none := powerSeries([]float64{1, 2}, []float64{-1, -1}); none != nil {
		t.Error("a power series without power")
	}
}

func TestBestPowerOver(t *testing.T) {
	perSecond := []float64{100, 100, 400, 500, 300, 100}
	watts, start, found := bestPowerOver(perSecond, 2)
	if !found || watts != 450 || start != 2 {
		t.Errorf("best 2 s: %v W from %d, want 450 W from 2", watts, start)
	}
	if watts, _, _ := bestPowerOver(perSecond, 6); watts != 250 {
		t.Errorf("best 6 s: %v W, want 250", watts)
	}
	if _, _, found := bestPowerOver(perSecond, 7); found {
		t.Error("a power over a duration longer than the series")
	}
	if _, _, found := bestPowerOver(make([]float64, 10), 5); found {
		t.Error("a best power of 0 W")
	}
}

func TestMergeEfforts(t *testing.T) {
	run := func(target, value float64) types.BestEffort {
		return types.BestEffort{Sport: "running", Kind: types.EffortDistance, Target: target, Value: value}
	}
	power := types.BestEffort{Sport: "running", Kind: types.EffortPower, Target: 60, Value: 300}
	// a duathlon: the second run leg has the best kilometer, the first one the best 5 km
	merged := mergeEfforts(types.BestEffortArray{}, types.BestEffortArray{run(1000, 240), run(5000, 1300), power})
	merged = mergeEfforts(merged, types.BestEffortArray{run(1000, 230), run(5000, 1400),
		{Sport: "running", Kind: types.EffortPower, Target: 60, Value: 280}})
	want := types.BestEffortArray{run(1000, 230), run(5000, 1300), power}
	if fmt.Sprint(merged) != fmt.Sprint(want) {
		t.Errorf("merged: %v, want %v", merged, want)
	}
}

func TestStoredBestEfforts(t *testing.T) {
	times, distances := steady(1200, 4) // 4800 m
	activity := models.Activity{Sport: "running", Distances: distances}
	for _, s := range times {
		activity.TimeStamps = append(activity.TimeStamps, fmt.Sprintf("%f", s))
	}
	// a record without distance, stored as 0
	activity.Distances[600] = 0
	efforts := StoredBestEfforts(activity)
	targets := map[float64]float64{400: 100, 1000: 250, 1609.344: 1609.344 / 4, 5000: -1}
	for target, seconds := range targets {
		var got *types.BestEffort
		for i := range efforts {
			if efforts[i].Target == target {
				got = &efforts[i]
			}
		}
		if seconds < 0 {
			if got != nil {
				t.Errorf("%v m in a 4800 m activity", target)
			}
			continue
		}
		if got == nil || math.Abs(got.Value-round1(seconds)) > 0.05 {
			t.Errorf("%v m: %+v, want %v s", target, got, seconds)
		}
	}
	activity.Legs = types.LegArray{{Sport: "running"}}
	if efforts := StoredBestEfforts(activity); efforts == nil || len(efforts) != 0 {
		t.Errorf("a multisport activity: %v, want none (not nil)", efforts)
	}
}

// TestBestEffortsFiles checks the efforts of the FIT files of prototypes/: consistent with the totals of
// the activity, faster over the short distances, more powerful over the short durations
func TestBestEffortsFiles(t *testing.T) {
	files, _ := filepath.Glob("../../prototypes/*.fit")
	if len(files) == 0 {
		t.Skip("FIT files missing in prototypes/")
	}
	checked := 0
	for _, file := range files {
		activity, err := SumAnalyze(file, models.Users{}, models.Equipments{})
		if err != nil {
			continue
		}
		if activity.BestEfforts == nil {
			t.Errorf("%s: best efforts not computed", file)
			continue
		}
		previous := map[string]types.BestEffort{}
		for _, e := range activity.BestEfforts {
			checked++
			if e.StartOffset < 0 || e.Value <= 0 {
				t.Errorf("%s: %+v", file, e)
			}
			key := e.Sport + e.Kind
			if p, found := previous[key]; found {
				switch e.Kind {
				case types.EffortDistance:
					// the efforts are in increasing targets: the pace over a longer distance isn't faster
					// than over a shorter one... by more than a few percent, the windows differ
					if e.Value/e.Target < 0.97*p.Value/p.Target {
						t.Errorf("%s: %v m at %.2f s/m, faster than %v m at %.2f s/m", file, e.Target,
							e.Value/e.Target, p.Target, p.Value/p.Target)
					}
				case types.EffortPower:
					if e.Value > p.Value {
						t.Errorf("%s: %v W over %v s, more than %v W over %v s", file, e.Value, e.Target,
							p.Value, p.Target)
					}
				}
			}
			previous[key] = e
			if e.Kind == types.EffortDistance && len(activity.Legs) == 0 {
				if e.Target > activity.Distance+1 {
					t.Errorf("%s: %v m in a %v m activity", file, e.Target, activity.Distance)
				}
				if max := EffortSports[e.Sport].MaxSpeed; e.Target/e.Value > max {
					t.Errorf("%s: %v m at %.1f m/s", file, e.Target, e.Target/e.Value)
				}
			}
		}
	}
	if checked == 0 {
		t.Skip("no best effort in the FIT files of prototypes/")
	}
	t.Logf("%d best efforts checked", checked)
}

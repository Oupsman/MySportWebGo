package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
)

// powerStream returns a stream of 1 Hz samples: watts during seconds, from start
func powerStream(times, powers []float64, start float64, watts float64, seconds int) ([]float64, []float64) {
	for s := 0; s < seconds; s++ {
		times = append(times, start+float64(s))
		powers = append(powers, watts)
	}
	return times, powers
}

func TestPowerMetricsSteady(t *testing.T) {
	// an hour at the FTP: IF 1, TSS 100
	times, powers := powerStream(nil, nil, 0, 250, 3601)
	m := ComputePowerMetrics(times, powers, 250)
	if m.NormalizedPower != 250 || m.Intensity != 1 || m.TSS != 100 {
		t.Errorf("NP %.1f, IF %.3f, TSS %.1f", m.NormalizedPower, m.Intensity, m.TSS)
	}
	// 250 W at an FTP of 250 is in zone 4 (91 to 105%)
	want := types.Uint16Array{0, 140, 190, 228, 265, 303, 378}
	for i := range want {
		if m.Bounds[i] != want[i] {
			t.Fatalf("bounds %v, want %v", m.Bounds, want)
		}
	}
	if m.Zones[3] != 3600 || m.Zones[2]+m.Zones[4] != 0 {
		t.Errorf("zones %v", m.Zones)
	}
}

func TestPowerMetricsIntervals(t *testing.T) {
	// 30 min at 100 W then 30 min at 300 W, FTP 200: the normalized power is above the average (200 W)
	times, powers := powerStream(nil, nil, 0, 100, 1800)
	times, powers = powerStream(times, powers, 1800, 300, 1801)
	m := ComputePowerMetrics(times, powers, 200)
	// by hand: the 30 s averages are 100 W, then a ramp of 29 values, then 300 W
	var fourth float64
	n := 0
	perSecond := append(make([]float64, 0, 3600), powers[1:]...)
	for i := npWindow - 1; i < len(perSecond); i++ {
		sum := 0.0
		for _, p := range perSecond[i-npWindow+1 : i+1] {
			sum += p
		}
		fourth += math.Pow(sum/npWindow, 4)
		n++
	}
	np := math.Round(math.Pow(fourth/float64(n), 0.25)*10) / 10
	if m.NormalizedPower != np || np <= 240 {
		t.Errorf("NP %.1f, want %.1f", m.NormalizedPower, np)
	}
	intensity := np / 200
	if math.Abs(m.TSS-3600*intensity*intensity/36) > 0.2 {
		t.Errorf("TSS %.1f", m.TSS)
	}
	// 100 W is zone 1 (below 112), 300 W zone 6 (242 to 302)
	if m.Zones[0] != 1799 || m.Zones[5] != 1801 {
		t.Errorf("zones %v", m.Zones)
	}
}

func TestPowerMetricsPausesAndShort(t *testing.T) {
	// 20 min, a pause of 10 min, 20 min: the pause counts nowhere
	times, powers := powerStream(nil, nil, 0, 200, 1200)
	times, powers = powerStream(times, powers, 1800, 200, 1200)
	m := ComputePowerMetrics(times, powers, 200)
	total := 0.0
	for _, z := range m.Zones {
		total += z
	}
	if total != 2398 || m.NormalizedPower != 200 {
		t.Errorf("time %.0f s, NP %.1f", total, m.NormalizedPower)
	}
	// less than a minute: zones, no normalized power
	times, powers = powerStream(nil, nil, 0, 200, 40)
	if m := ComputePowerMetrics(times, powers, 200); m.NormalizedPower != 0 || m.Zones[3] != 39 {
		t.Errorf("short: %+v", m)
	}
	// no FTP: nothing
	if m := ComputePowerMetrics(times, powers, 0); m.Bounds != nil || m.Zones[3] != 0 {
		t.Errorf("no FTP: %+v", m)
	}
}

func rideWithPower(watts uint16, seconds int) models.Activity {
	a := models.Activity{Sport: "cycling", Date: time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)}
	for s := 0; s <= seconds; s++ {
		a.Powers = append(a.Powers, watts)
		a.PowerTS = append(a.PowerTS, formatSeconds(float64(s)))
	}
	return a
}

func formatSeconds(s float64) string { return strconv.FormatFloat(s, 'f', 6, 64) }

func TestTrainingScoreSources(t *testing.T) {
	fixed := func(ftp uint16) FTPFunc { return func(time.Time, float64) (uint16, error) { return ftp, nil } }

	// power and FTP: the TSS
	a := rideWithPower(250, 3600)
	if err := setPowerMetrics(&a, fixed(250)); err != nil {
		t.Fatal(err)
	}
	if err := setTrainingScore(&a, nil, false, nil); err != nil {
		t.Fatal(err)
	}
	if a.ScoreSource != models.ScorePower || a.Trimp == nil || *a.Trimp != 100 || a.FTP != 250 {
		t.Errorf("power: %s %v ftp %d", a.ScoreSource, a.Trimp, a.FTP)
	}

	// the device's score first
	session := mesgdef.NewSession(nil).SetTrainingStressScoreScaled(80)
	if err := setTrainingScore(&a, []*mesgdef.Session{session}, false, nil); err != nil {
		t.Fatal(err)
	}
	if a.ScoreSource != models.ScoreDevice || *a.Trimp != 80 {
		t.Errorf("device: %s %v", a.ScoreSource, *a.Trimp)
	}

	// no FTP: the TRIMP, and no power metrics; a run's power doesn't count
	for name, ftp := range map[string]FTPFunc{"no FTP": fixed(0), "nil": nil} {
		b := rideWithPower(250, 3600)
		if err := setPowerMetrics(&b, ftp); err != nil {
			t.Fatal(err)
		}
		if err := setTrainingScore(&b, nil, false, nil); err != nil {
			t.Fatal(err)
		}
		if b.ScoreSource != models.ScoreHR || b.TSS != nil || len(b.PowerZones) != models.PowerZones {
			t.Errorf("%s: %s %v %v", name, b.ScoreSource, b.TSS, b.PowerZones)
		}
	}
	run := rideWithPower(250, 3600)
	run.Sport = "running"
	if err := setPowerMetrics(&run, fixed(250)); err != nil || run.TSS != nil || run.FTP != 0 {
		t.Errorf("run: %v %v %d", err, run.TSS, run.FTP)
	}
}

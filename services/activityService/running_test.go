package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

// a run with a chest strap whose running dynamics start at the third record; it records no stance time
// percent, and loses the vertical ratio of the fourth record
func runWithDynamics(start time.Time) *filedef.Activity {
	fit := filedef.NewActivity()
	fit.Sessions = []*mesgdef.Session{mesgdef.NewSession(nil).SetStartTime(start).SetSport(typedef.SportRunning)}
	for i := range 6 {
		record := mesgdef.NewRecord(nil).SetTimestamp(start.Add(time.Duration(i) * time.Second)).
			SetDistance(uint32(i * 3 * 100))
		if i >= 2 {
			// 82..85 mm, 250 ms, 49.5 %, 1100 mm
			record.SetVerticalOscillation(uint16(80+i) * 10).SetStanceTime(2500).SetStanceTimeBalance(4950).SetStepLength(11000)
			if i != 3 {
				record.SetVerticalRatio(750) // 7.5 %
			}
		}
		fit.Records = append(fit.Records, record)
	}
	return fit
}

func TestRunningDynamics(t *testing.T) {
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	activity, err := AnalyzeRecords(runWithDynamics(start), models.Activity{Distance: 15},
		models.AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	running := activity.Metrics.Running
	if running == nil {
		t.Fatal("no running dynamics")
	}
	// the loop of AnalyzeRecords leaves the last record out
	if got := running.VerticalOscillations.Values; len(got) != 3 || got[0] != 82 || got[2] != 84 {
		t.Errorf("vertical oscillations %v, want [82 83 84]", got)
	}
	if got := running.VerticalOscillations.TimeStamps; len(got) != 3 || got[0] != 2 {
		t.Errorf("vertical oscillation time stamps %v, want from 2 s", got)
	}
	if got := running.VerticalRatios.TimeStamps; len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Errorf("vertical ratio time stamps %v, want [2 4]", got)
	}
	if running.StanceTimePercents != nil {
		t.Errorf("stance time percents %v, want none", running.StanceTimePercents)
	}
	if math.Abs(running.AvgVerticalOscillation-83) > 1e-9 || running.AvgStanceTime != 250 ||
		math.Abs(running.AvgStanceTimeBalance-49.5) > 1e-9 || running.AvgStepLength != 1100 ||
		running.AvgVerticalRatio != 7.5 || running.AvgStanceTimePercent != 0 {
		t.Errorf("averages %+v", running)
	}
}

func TestNoRunningDynamics(t *testing.T) {
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	activity, err := AnalyzeRecords(runWithoutFixAtEnds(start, 10), models.Activity{Distance: 1100},
		models.AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	if activity.Metrics.Running != nil {
		t.Fatalf("running dynamics %+v, want none", activity.Metrics.Running)
	}
	stored, err := activity.Metrics.Value()
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.([]byte)) != "{}" {
		t.Errorf("stored metrics %s, want {}", stored)
	}
}

func TestMetricsScan(t *testing.T) {
	var m types.ActivityMetrics
	if err := m.Scan(nil); err != nil || m.Running != nil {
		t.Fatalf("NULL scanned as %+v, %v", m, err)
	}
	stored, _ := json.Marshal(types.ActivityMetrics{Running: &types.RunningMetrics{AvgStanceTime: 250}})
	if err := m.Scan(stored); err != nil || m.Running == nil || m.Running.AvgStanceTime != 250 {
		t.Fatalf("%s scanned as %+v, %v", stored, m.Running, err)
	}
}

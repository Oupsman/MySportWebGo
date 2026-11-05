package activityService

import (
	"MySportWeb/internal/pkg/models"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

func timerEvent(at time.Time, eventType typedef.EventType) *mesgdef.Event {
	return mesgdef.NewEvent(nil).SetTimestamp(at).SetEvent(typedef.EventTimer).SetEventType(eventType)
}

func TestTimerTime(t *testing.T) {
	start := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return start.Add(time.Duration(s) * time.Second) }
	paused := []*mesgdef.Event{
		timerEvent(at(0), typedef.EventTypeStart),
		timerEvent(at(100), typedef.EventTypeStop),
		// another kind of event doesn't count
		mesgdef.NewEvent(nil).SetTimestamp(at(150)).SetEvent(typedef.EventLap).SetEventType(typedef.EventTypeStop),
		timerEvent(at(200), typedef.EventTypeStart),
		timerEvent(at(400), typedef.EventTypeStopAll),
	}
	for _, tt := range []struct {
		name   string
		events []*mesgdef.Event
		to     int
		want   time.Duration
	}{
		{"pause then cut while running", paused, 300, 200 * time.Second},
		{"cut during the pause", paused, 150, 100 * time.Second},
		{"no timer event", nil, 300, 300 * time.Second},
		{"starting with a stop", []*mesgdef.Event{timerEvent(at(50), typedef.EventTypeStop)}, 100, 50 * time.Second},
	} {
		if got := timerTime(tt.events, start, at(tt.to)); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestTrimRide(t *testing.T) {
	path := filepath.Join("../../prototypes", triathlonFiles[1])
	if _, err := os.Stat(path); err != nil {
		t.Skip("FIT files missing in prototypes/")
	}
	user := models.Users{SecurityDistance: 500}
	full, err := SumAnalyze(path, user, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	if full.TrimEnd != 0 || full.RecordedDuration <= 0 {
		t.Fatalf("whole ride: trim end %v, recorded duration %v", full.TrimEnd, full.RecordedDuration)
	}

	end := math.Round(full.RecordedDuration / 2)
	trimmed, err := AnalyzeTrimmed(path, user, models.Equipments{}, end)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed.TrimEnd != end || trimmed.RecordedDuration != full.RecordedDuration {
		t.Errorf("trim end %v recorded %v, want %v and %v", trimmed.TrimEnd, trimmed.RecordedDuration, end, full.RecordedDuration)
	}
	if !trimmed.Date.Equal(full.Date) || trimmed.Sport != full.Sport {
		t.Errorf("date %v sport %s, want %v %s", trimmed.Date, trimmed.Sport, full.Date, full.Sport)
	}
	if float64(trimmed.Duration) > end || trimmed.Duration >= full.Duration || trimmed.Duration == 0 {
		t.Errorf("duration %d, want at most %v and below %d", trimmed.Duration, end, full.Duration)
	}
	if trimmed.Distance <= 0 || trimmed.Distance >= full.Distance {
		t.Errorf("distance %v, want below %v", trimmed.Distance, full.Distance)
	}
	if trimmed.Calories > full.Calories || trimmed.TotalAscent > full.TotalAscent+5 {
		t.Errorf("calories %d ascent %d, more than the whole ride %d %d", trimmed.Calories, trimmed.TotalAscent, full.Calories, full.TotalAscent)
	}
	if trimmed.AvgSpeed <= 0 || trimmed.MaxSpeed > full.MaxSpeed {
		t.Errorf("average speed %v max %v (whole ride max %v)", trimmed.AvgSpeed, trimmed.MaxSpeed, full.MaxSpeed)
	}
	if len(trimmed.GpsPoints) == 0 || len(trimmed.GpsPoints) >= len(full.GpsPoints) {
		t.Errorf("%d GPS points, the whole ride has %d", len(trimmed.GpsPoints), len(full.GpsPoints))
	}
	for name, stamps := range map[string][]float64{
		"gps": trimmed.GpsTimeStamps, "heart": trimmed.HeartTimeStamps, "cadence": trimmed.CadenceTimeStamps,
	} {
		if n := len(stamps); n > 0 && stamps[n-1] > end {
			t.Errorf("%s: last time stamp %v after the end %v", name, stamps[n-1], end)
		}
	}

	// at or after the end of the recording, nothing is trimmed
	whole, err := AnalyzeTrimmed(path, user, models.Equipments{}, full.RecordedDuration+10)
	if err != nil {
		t.Fatal(err)
	}
	if whole.TrimEnd != 0 || whole.Distance != full.Distance || whole.Duration != full.Duration {
		t.Errorf("trimmed after the end: trim end %v distance %v duration %d", whole.TrimEnd, whole.Distance, whole.Duration)
	}

	if _, err := AnalyzeTrimmed(path, user, models.Equipments{}, TrimMinDuration-1); !errors.Is(err, ErrTrimTooShort) {
		t.Errorf("trimmed to %d s: error %v, want ErrTrimTooShort", TrimMinDuration-1, err)
	}
}

func TestTrimMultisport(t *testing.T) {
	path := multisportFile(t, triathlonFiles, triathlonSports, 0)
	user := models.Users{SecurityDistance: 500}
	full, err := SumAnalyze(path, user, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	bike := full.Legs[1]
	// in the middle of the bike leg: the run is dropped, the bike leg cut
	end := math.Round(bike.StartOffset + bike.Duration/2)
	trimmed, err := AnalyzeTrimmed(path, user, models.Equipments{}, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(trimmed.Legs) != 2 {
		t.Fatalf("%d legs, want the swim and the bike", len(trimmed.Legs))
	}
	if trimmed.Sport == "triathlon" {
		t.Error("still a triathlon without the run")
	}
	if trimmed.Legs[0].Distance != full.Legs[0].Distance || trimmed.Legs[0].Duration != full.Legs[0].Duration {
		t.Errorf("the swim changed: %+v, want %+v", trimmed.Legs[0], full.Legs[0])
	}
	cut := trimmed.Legs[1]
	if cut.Distance <= 0 || cut.Distance >= bike.Distance || cut.Duration >= bike.Duration {
		t.Errorf("bike leg %v m in %v s, the whole one %v m in %v s", cut.Distance, cut.Duration, bike.Distance, bike.Duration)
	}
	if trimmed.Distance >= full.Distance {
		t.Errorf("distance %v, the triathlon %v", trimmed.Distance, full.Distance)
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
)

var trimpDate = time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

// steadyActivity returns an activity of minutes at a constant heart rate, sampled every second
func steadyActivity(user models.Users, minutes int, hr uint16) models.Activity {
	activity := models.Activity{User: user, Date: trimpDate}
	for s := 0; s <= minutes*60; s++ {
		activity.Hearts = append(activity.Hearts, hr)
		activity.HeartTimeStamps = append(activity.HeartTimeStamps, float64(s))
	}
	return activity
}

func TestBanisterTRIMP(t *testing.T) {
	man := models.Users{MaxHR: 190, Gender: 0}
	woman := models.Users{MaxHR: 190, Gender: 1}
	cases := []struct {
		name     string
		activity models.Activity
		restHR   uint16
		want     float64
	}{
		// x = 0.5: 60 × 0.5 × 0.64 × e^0.96
		{"man, 1h at half the reserve", steadyActivity(man, 60, 125), 60, 50.1},
		// 60 × 0.5 × 0.86 × e^0.835
		{"woman, 1h at half the reserve", steadyActivity(woman, 60, 125), 60, 59.5},
		{"below the resting heart rate counts nothing", steadyActivity(man, 60, 50), 60, 0},
		// x is capped at 1: 10 × 0.64 × e^1.92
		{"above the maximum counts as the maximum", steadyActivity(man, 10, 200), 60, 43.7},
		{"no heart rate", models.Activity{User: man, Date: trimpDate}, 60, 0},
		{"maximum below the resting heart rate", steadyActivity(models.Users{MaxHR: 55}, 60, 125), 60, 0},
		// no date of birth: 220 - 2026 years old wraps around
		{"maximum unknown", steadyActivity(models.Users{}, 60, 125), 60, 0},
	}
	for _, c := range cases {
		if got := BanisterTRIMP(c.activity, c.restHR); got != c.want {
			t.Errorf("%s: TRIMP %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBanisterTRIMPHigherRestHR(t *testing.T) {
	// the same heart rate is a smaller effort for a user with a higher resting heart rate:
	// x = (125-80)/(190-80) = 0.41 instead of 0.5
	user := models.Users{MaxHR: 190}
	if low, high := BanisterTRIMP(steadyActivity(user, 60, 125), 60),
		BanisterTRIMP(steadyActivity(user, 60, 125), 80); high >= low {
		t.Errorf("TRIMP %v with a resting heart rate of 80, not below %v with 60", high, low)
	}
}

func TestBanisterTRIMPGaps(t *testing.T) {
	user := models.Users{MaxHR: 190}
	activity := steadyActivity(user, 30, 125)
	// a 20 minutes pause, then 30 minutes more: the pause doesn't count
	last := activity.HeartTimeStamps[len(activity.HeartTimeStamps)-1]
	for s := 0; s <= 30*60; s++ {
		activity.Hearts = append(activity.Hearts, 125)
		activity.HeartTimeStamps = append(activity.HeartTimeStamps, last+20*60+float64(s))
	}
	if got, want := BanisterTRIMP(activity, 60), BanisterTRIMP(steadyActivity(user, 60, 125), 60); math.Abs(got-want) > 0.1 {
		t.Errorf("TRIMP %v with a pause, want %v as without it", got, want)
	}
	// sparse samples (every 5 s) count their whole interval
	sparse := models.Activity{User: user, Date: trimpDate}
	for s := 0; s <= 3600; s += 5 {
		sparse.Hearts = append(sparse.Hearts, 125)
		sparse.HeartTimeStamps = append(sparse.HeartTimeStamps, float64(s))
	}
	if got := BanisterTRIMP(sparse, 60); got != 50.1 {
		t.Errorf("TRIMP %v sampled every 5 s, want 50.1", got)
	}
	// series of different lengths can't be aligned
	activity.HeartTimeStamps = activity.HeartTimeStamps[1:]
	if got := BanisterTRIMP(activity, 60); got != 0 {
		t.Errorf("TRIMP %v with misaligned series, want 0", got)
	}
}

// fitSession returns a FIT session with the training load and TSS given, 0 for a missing field
func fitSession(load, tss float64) *mesgdef.Session {
	s := mesgdef.NewSession(nil)
	if load > 0 {
		s.TrainingLoadPeak = int32(load * 65536)
	}
	if tss > 0 {
		s.TrainingStressScore = uint16(tss * 10)
	}
	return s
}

func TestEmbeddedTrainingScore(t *testing.T) {
	for _, c := range []struct {
		name     string
		sessions []*mesgdef.Session
		want     float64
		ok       bool
	}{
		{"no score", []*mesgdef.Session{fitSession(0, 0)}, 0, false},
		{"Garmin's training load", []*mesgdef.Session{fitSession(87.25, 0)}, 87.3, true},
		{"the TSS without training load", []*mesgdef.Session{fitSession(0, 64.5)}, 64.5, true},
		{"the training load before the TSS", []*mesgdef.Session{fitSession(87, 64.5)}, 87, true},
		// a multisport activity: the transition has none
		{"the sessions add up", []*mesgdef.Session{fitSession(40, 0), fitSession(0, 0), fitSession(0, 30)}, 70, true},
	} {
		got, ok := embeddedTrainingScore(c.sessions)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %v %v, want %v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestSetTrainingScore(t *testing.T) {
	user := models.Users{MaxHR: 190}
	restHR80 := func(date time.Time) (uint16, error) {
		if !date.Equal(trimpDate) {
			t.Errorf("resting heart rate asked at %s, not at the date of the activity", date)
		}
		return 80, nil
	}
	never := func(time.Time) (uint16, error) { return 0, nil }
	failing := func(time.Time) (uint16, error) { return 0, errors.New("database down") }
	trimp60 := BanisterTRIMP(steadyActivity(user, 60, 125), 60)
	trimp80 := BanisterTRIMP(steadyActivity(user, 60, 125), 80)
	for _, c := range []struct {
		name     string
		sessions []*mesgdef.Session
		trimmed  bool
		restHR   RestHRFunc
		want     float64
	}{
		{"the device's score wins", []*mesgdef.Session{fitSession(120, 0)}, false, restHR80, 120},
		{"trimmed: the device scored the whole recording", []*mesgdef.Session{fitSession(120, 0)}, true, restHR80, trimp80},
		{"no score recorded: the TRIMP", []*mesgdef.Session{fitSession(0, 0)}, false, restHR80, trimp80},
		{"resting heart rate never measured", []*mesgdef.Session{fitSession(0, 0)}, false, never, trimp60},
		{"no resting heart rate lookup", []*mesgdef.Session{fitSession(0, 0)}, false, nil, trimp60},
	} {
		activity := steadyActivity(user, 60, 125)
		if err := setTrainingScore(&activity, c.sessions, c.trimmed, c.restHR); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if activity.Trimp == nil || *activity.Trimp != c.want {
			t.Errorf("%s: training score %v, want %v", c.name, activity.Trimp, c.want)
		}
	}
	activity := steadyActivity(user, 60, 125)
	if err := setTrainingScore(&activity, []*mesgdef.Session{fitSession(0, 0)}, false, failing); err == nil {
		t.Error("the error of the resting heart rate lookup is lost")
	}
	// the device's score doesn't need the resting heart rate
	if err := setTrainingScore(&activity, []*mesgdef.Session{fitSession(50, 0)}, false, failing); err != nil {
		t.Errorf("resting heart rate looked up for a recorded score: %v", err)
	}
}

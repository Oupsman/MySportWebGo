package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

func TestMultisportName(t *testing.T) {
	legs := func(sports ...string) []types.Leg {
		l := make([]types.Leg, len(sports))
		for i, sport := range sports {
			l[i].Sport = sport
		}
		return l
	}
	for _, test := range []struct {
		legs []types.Leg
		want string
	}{
		{legs("swimming", "cycling", "running"), "triathlon"},
		{legs("running", "cycling", "running"), "duathlon"},
		{legs("swimming", "running"), "aquathlon"},
		{legs("cycling", "running"), "multisport"},
		// a file resumed after a stop: one sport, it stays a ride
		{legs("cycling", "cycling"), "cycling"},
	} {
		if got := MultisportName(test.legs); got != test.want {
			t.Errorf("%v: %s, want %s", test.legs, got, test.want)
		}
	}
}

func session(sport typedef.Sport, start time.Time, seconds float64, meters float64) *mesgdef.Session {
	return mesgdef.NewSession(nil).SetSport(sport).SetStartTime(start).
		SetTotalTimerTime(uint32(seconds * 1000)).SetTotalElapsedTime(uint32(seconds * 1000)).
		SetTotalDistance(uint32(meters * 100))
}

func TestAnalyzeLegs(t *testing.T) {
	start := time.Date(2026, 6, 14, 7, 0, 0, 0, time.UTC)
	sessions := []*mesgdef.Session{
		session(typedef.SportSwimming, start, 1800, 1900).SetAvgHeartRate(140),
		session(typedef.SportTransition, start.Add(1800*time.Second), 120, 200),
		session(typedef.SportCycling, start.Add(1920*time.Second), 9000, 90000).SetAvgHeartRate(150).SetAvgPower(200),
		session(typedef.SportTransition, start.Add(10920*time.Second), 60, 100),
		session(typedef.SportRunning, start.Add(10980*time.Second), 5400, 21100).SetAvgHeartRate(160).SetAvgPower(250),
	}
	legs := AnalyzeLegs(sessions)
	if len(legs) != 3 {
		t.Fatalf("%d legs, want 3 (transitions are not legs)", len(legs))
	}
	if legs[0].TransitionTime != 0 || legs[1].TransitionTime != 120 || legs[2].TransitionTime != 60 {
		t.Errorf("transitions %v %v %v, want 0 120 60", legs[0].TransitionTime, legs[1].TransitionTime, legs[2].TransitionTime)
	}
	if legs[2].StartOffset != 10980 || legs[1].Distance != 90000 || legs[0].AvgPower != 0 {
		t.Errorf("leg values: %+v", legs)
	}

	var activity models.Activity
	summarizeLegs(&activity, sessions, legs)
	if activity.Sport != "triathlon" || !activity.Date.Equal(start) {
		t.Errorf("sport %s date %s", activity.Sport, activity.Date)
	}
	// the transitions are part of the race
	if activity.Duration != 16380 || activity.Distance != 113300 {
		t.Errorf("duration %d distance %v, want 16380 113300", activity.Duration, activity.Distance)
	}
	// weighted by the time of each leg: (140*1800 + 150*9000 + 160*5400) / 16200
	if activity.AvgHR != 152 {
		t.Errorf("avg HR %d, want 152", activity.AvgHR)
	}
	// running and cycling watts don't add up
	if activity.AvgPower != 0 {
		t.Errorf("avg power %d, want none across sports", activity.AvgPower)
	}
}

func TestContinuousDistances(t *testing.T) {
	start := time.Date(2026, 6, 14, 7, 0, 0, 0, time.UTC)
	sessions := []*mesgdef.Session{
		session(typedef.SportCycling, start, 20, 0),
		session(typedef.SportRunning, start.Add(20*time.Second), 20, 0),
	}
	record := func(second int, meters float64) *mesgdef.Record {
		return mesgdef.NewRecord(nil).SetTimestamp(start.Add(time.Duration(second) * time.Second)).
			SetDistance(uint32(meters * 100))
	}
	// the device restarts the distance on the second session
	restarted := []*mesgdef.Record{record(0, 0), record(10, 100), record(19, 200), record(20, 0), record(30, 50)}
	continuousDistances(restarted, sessions)
	// another device keeps counting
	kept := []*mesgdef.Record{record(0, 0), record(19, 200), record(20, 201), record(30, 250)}
	continuousDistances(kept, sessions)
	for _, test := range []struct {
		records []*mesgdef.Record
		want    []float64
	}{
		{restarted, []float64{0, 100, 200, 200, 250}},
		{kept, []float64{0, 200, 201, 250}},
	} {
		for i, r := range test.records {
			if r.DistanceScaled() != test.want[i] {
				t.Errorf("record %d: %v, want %v", i, r.DistanceScaled(), test.want[i])
			}
		}
	}
}

// multisportFile writes a triathlon made of real rides relabelled swim, bike and run, with a transition
// between them, and the distance restarting on each leg like the devices do. With bikePower, the
// cycling leg has a power meter giving that power.
func multisportFile(t *testing.T, names []string, sports []typedef.Sport, bikePower uint16) string {
	transition := 90 * time.Second
	combined := filedef.NewActivity()
	var end time.Time
	for i, name := range names {
		f, err := os.Open(filepath.Join("../../prototypes", name))
		if err != nil {
			t.Skip("FIT files missing in prototypes/")
		}
		dec := decoder.New(f, decoder.WithIgnoreChecksum())
		fit, err := dec.Decode()
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		part := filedef.NewActivity(fit.Messages...)
		s := part.Sessions[0]
		if i == 0 {
			combined.FileId = part.FileId
			end = s.StartTime
		} else {
			// the transition before the leg
			combined.Sessions = append(combined.Sessions, session(typedef.SportTransition, end, transition.Seconds(), 0).
				SetTimestamp(end.Add(transition)).SetEvent(typedef.EventSession).SetEventType(typedef.EventTypeStop))
			end = end.Add(transition)
		}
		shift := end.Sub(s.StartTime)
		for _, r := range part.Records {
			r.Timestamp = r.Timestamp.Add(shift)
		}
		s.StartTime = s.StartTime.Add(shift)
		s.Timestamp = s.Timestamp.Add(shift)
		s.Sport = sports[i]
		s.SubSport = typedef.SubSportGeneric
		if bikePower > 0 && s.Sport == typedef.SportCycling {
			s.AvgPower = bikePower
			for _, r := range part.Records {
				r.Power = bikePower
			}
		}
		combined.Records = append(combined.Records, part.Records...)
		combined.Sessions = append(combined.Sessions, s)
		end = part.Records[len(part.Records)-1].Timestamp.Add(time.Second)
	}
	combined.Activity = mesgdef.NewActivity(nil).SetTimestamp(end).SetNumSessions(uint16(len(combined.Sessions))).
		SetType(typedef.ActivityManual).SetEvent(typedef.EventActivity).SetEventType(typedef.EventTypeStop)

	path := filepath.Join(t.TempDir(), "triathlon.fit")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	fit := combined.ToFIT(nil)
	if err := encoder.New(out).Encode(&fit); err != nil {
		t.Fatal(err)
	}
	return path
}

var triathlonFiles = []string{"14d57cc7-3494-4e13-af9b-5cc3c95ec4cb.fit", "2022-08-30-18-12-37.fit", "1e893571-c023-431d-8443-c42236c415e7.fit"}
var triathlonSports = []typedef.Sport{typedef.SportSwimming, typedef.SportCycling, typedef.SportRunning}

func TestMultisportFile(t *testing.T) {
	path := multisportFile(t, triathlonFiles, triathlonSports, 0)

	activity, err := SumAnalyze(path, models.Users{SecurityDistance: 500}, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	if activity.Sport != "triathlon" || len(activity.Legs) != 3 {
		t.Fatalf("sport %s with %d legs, want a triathlon of 3 legs", activity.Sport, len(activity.Legs))
	}
	var legsDistance, legsTime float64
	for i, leg := range activity.Legs {
		legsDistance += leg.Distance
		legsTime += leg.Duration + leg.TransitionTime
		if (i == 0) != (leg.TransitionTime == 0) || (i > 0 && leg.TransitionTime != 90) {
			t.Errorf("leg %d: transition %v", i, leg.TransitionTime)
		}
	}
	if math.Abs(activity.Distance-legsDistance) > 1 || math.Abs(float64(activity.Duration)-legsTime) > 1 {
		t.Errorf("activity %v m %d s, legs %v m %v s", activity.Distance, activity.Duration, legsDistance, legsTime)
	}
	// one trace and one growing distance over the three legs
	for i := 1; i < len(activity.Distances); i++ {
		if activity.Distances[i] < activity.Distances[i-1] && activity.Distances[i] != 0 {
			t.Fatalf("distance going back at %d: %v after %v", i, activity.Distances[i], activity.Distances[i-1])
		}
	}
	last := activity.Distances[len(activity.Distances)-1]
	if math.Abs(last-activity.Distance) > activity.Distance*0.05 {
		t.Errorf("last record distance %v, activity distance %v", last, activity.Distance)
	}
	if len(activity.GpsPoints) < 1000 || activity.StartPosition == nil {
		t.Errorf("%d GPS points", len(activity.GpsPoints))
	}

	// the power is estimated on the bike leg only, there is no power meter
	swim, bike, run := activity.Legs[0], activity.Legs[1], activity.Legs[2]
	if len(activity.Powers) == 0 || len(activity.Powers) != len(activity.PowerTS) {
		t.Fatalf("%d powers for %d time stamps", len(activity.Powers), len(activity.PowerTS))
	}
	for _, ts := range activity.PowerTS {
		offset, err := strconv.ParseFloat(ts, 64)
		if err != nil {
			t.Fatal(err)
		}
		if offset < bike.StartOffset || offset >= run.StartOffset {
			t.Fatalf("power at %vs, outside of the bike leg [%v, %v)", offset, bike.StartOffset, run.StartOffset)
		}
	}
	if bike.AvgPower == 0 || !bike.PowerEstimated {
		t.Errorf("bike leg: power %d estimated %v", bike.AvgPower, bike.PowerEstimated)
	}
	if swim.AvgPower != 0 || run.AvgPower != 0 || swim.PowerEstimated || run.PowerEstimated {
		t.Errorf("power on the swim %d or the run %d", swim.AvgPower, run.AvgPower)
	}
	// the power of the bike leg isn't the one of the triathlon, and a triathlon isn't a commute
	if activity.AvgPower != 0 || activity.Co2 != 0 {
		t.Errorf("activity power %d, co2 %v", activity.AvgPower, activity.Co2)
	}
}

func TestMultisportFileWithPowerMeter(t *testing.T) {
	path := multisportFile(t, triathlonFiles, triathlonSports, 180)

	activity, err := SumAnalyze(path, models.Users{SecurityDistance: 500}, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Legs) != 3 {
		t.Fatalf("%d legs", len(activity.Legs))
	}
	bike := activity.Legs[1]
	// the measured power is kept, not estimated
	if bike.AvgPower != 180 || bike.PowerEstimated {
		t.Errorf("bike leg: power %d estimated %v, want the measured 180", bike.AvgPower, bike.PowerEstimated)
	}
	for _, p := range activity.Powers {
		if p != 180 {
			t.Fatalf("power %d in the series, want only the measured 180", p)
		}
	}
	if len(activity.Powers) == 0 {
		t.Error("the measured power series is missing")
	}
}

func TestSumAnalyzeRefusesFileWithoutSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.fit")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	empty := filedef.NewActivity()
	empty.FileId = *mesgdef.NewFileId(nil).SetType(typedef.FileActivity)
	fit := empty.ToFIT(nil)
	if err := encoder.New(out).Encode(&fit); err != nil {
		t.Fatal(err)
	}
	out.Close()
	if _, err := SumAnalyze(path, models.Users{}, models.Equipments{}); err == nil {
		t.Fatal("a file without session was accepted")
	}
}

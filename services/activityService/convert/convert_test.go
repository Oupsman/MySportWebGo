package convert

import (
	"bytes"
	"compress/gzip"
	"errors"
	"math"
	"testing"

	"MySportWeb/services/activityService/convert/converttest"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/typedef"
)

func decodeFIT(t *testing.T, content []byte) *filedef.Activity {
	t.Helper()
	dec := decoder.New(bytes.NewReader(content))
	fit, err := dec.Decode()
	if err != nil {
		t.Fatalf("the converted file isn't a valid FIT file: %v", err)
	}
	return filedef.NewActivity(fit.Messages...)
}

func near(got, want, tolerance float64) bool { return math.Abs(got-want) <= tolerance }

func TestConvertGPX(t *testing.T) {
	result, err := Convert(converttest.GPX(true), typedef.SportRunning)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sport != typedef.SportCycling || !result.SportFromFile {
		t.Errorf("sport %s (from the file %v), want the file's cycling", result.Sport, result.SportFromFile)
	}
	a := decodeFIT(t, result.FIT)
	if len(a.Records) != 600 || len(a.Sessions) != 1 || len(a.Laps) != 1 {
		t.Fatalf("%d records, %d sessions, %d laps", len(a.Records), len(a.Sessions), len(a.Laps))
	}
	s := a.Sessions[0]
	if !s.StartTime.Equal(converttest.Start) || s.Sport != typedef.SportCycling {
		t.Errorf("start %v, sport %s", s.StartTime, s.Sport)
	}
	// 599 steps of 5 m; 598 s moving, the pause doesn't count; 718 s elapsed
	if d := s.TotalDistanceScaled(); !near(d, 599*5, 5) {
		t.Errorf("distance %.1f m", d)
	}
	if timer := s.TotalTimerTimeScaled(); timer != 598 {
		t.Errorf("timer %.1f s, want 598", timer)
	}
	if elapsed := s.TotalElapsedTimeScaled(); elapsed != 719 {
		t.Errorf("elapsed %.1f s, want 719", elapsed)
	}
	if v := s.EnhancedAvgSpeedScaled(); !near(v, 5, 0.1) {
		t.Errorf("average speed %.2f m/s", v)
	}
	if v := s.EnhancedMaxSpeedScaled(); !near(v, 5, 0.2) {
		t.Errorf("maximum speed %.2f m/s", v)
	}
	if !near(float64(s.TotalAscent), 299, 3) || s.TotalDescent != 0 {
		t.Errorf("ascent %d, descent %d", s.TotalAscent, s.TotalDescent)
	}
	if s.AvgHeartRate < 144 || s.AvgHeartRate > 145 || s.MaxHeartRate != 149 || s.AvgPower != 200 || s.AvgCadence != 85 {
		t.Errorf("hr %d/%d, power %d, cadence %d", s.AvgHeartRate, s.MaxHeartRate, s.AvgPower, s.AvgCadence)
	}
	r := a.Records[10]
	if r.HeartRate != 140 || r.Power != 200 || r.Cadence != 85 || !near(r.EnhancedAltitudeScaled(), 105, 0.01) {
		t.Errorf("record %+v", r)
	}
	if a.FileId.ProductName != "StravaGPX iPhone" {
		t.Errorf("creator %q", a.FileId.ProductName)
	}
	// the pause: the timer stops then starts again
	var stops int
	for _, e := range a.Events {
		if e.Event == typedef.EventTimer && e.EventType == typedef.EventTypeStopAll {
			stops++
		}
	}
	if stops != 2 {
		t.Errorf("%d timer stops, want the pause and the end", stops)
	}
}

func TestConvertGPXSport(t *testing.T) {
	// without type, the sport given; without either, generic
	result, err := Convert(converttest.GPX(false), typedef.SportHiking)
	if err != nil || result.Sport != typedef.SportHiking || result.SportFromFile {
		t.Errorf("given sport: %v %v %v", result.Sport, result.SportFromFile, err)
	}
	result, err = Convert(converttest.GPX(false), typedef.SportInvalid)
	if err != nil || result.Sport != typedef.SportGeneric {
		t.Errorf("no sport: %v %v", result.Sport, err)
	}
}

func TestConvertTCX(t *testing.T) {
	result, err := Convert(converttest.TCX(), typedef.SportCycling)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sport != typedef.SportRunning || !result.SportFromFile {
		t.Errorf("sport %s", result.Sport)
	}
	a := decodeFIT(t, result.FIT)
	s := a.Sessions[0]
	// the distance of the file
	if s.TotalDistanceScaled() != 897 || s.TotalTimerTimeScaled() != 299 || s.TotalCalories != 75 {
		t.Errorf("distance %.1f, timer %.1f, calories %d", s.TotalDistanceScaled(), s.TotalTimerTimeScaled(), s.TotalCalories)
	}
	if s.AvgHeartRate != 150 || s.AvgCadence != 88 || a.FileId.ProductName != "Forerunner 245" {
		t.Errorf("hr %d, cadence %d, creator %q", s.AvgHeartRate, s.AvgCadence, a.FileId.ProductName)
	}
}

func TestConvertRefused(t *testing.T) {
	for name, content := range map[string]string{
		"not xml":           "hello",
		"broken gpx":        `<gpx><trk><trkseg><trkpt lat="1"`,
		"empty gpx":         `<gpx creator="x"></gpx>`,
		"no time":           `<gpx><trk><trkseg><trkpt lat="1" lon="1"/><trkpt lat="1.1" lon="1"/></trkseg></trk></gpx>`,
		"one point":         `<gpx><trk><trkseg><trkpt lat="1" lon="1"><time>2025-06-01T07:00:00Z</time></trkpt></trkseg></trk></gpx>`,
		"tcx without point": `<TrainingCenterDatabase><Activities><Activity Sport="Running"></Activity></Activities></TrainingCenterDatabase>`,
	} {
		var refused *InvalidError
		if _, err := Convert([]byte(content), typedef.SportCycling); !errors.As(err, &refused) {
			t.Errorf("%s: %v, want an InvalidError", name, err)
		}
	}
}

func TestDetectAndGunzip(t *testing.T) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(converttest.TCX())
	w.Close()
	content, err := Gunzip(buf.Bytes())
	if err != nil || Detect(content) != TCX {
		t.Errorf("gzipped TCX: %v %v", Detect(content), err)
	}
	if plain, _ := Gunzip([]byte("abc")); string(plain) != "abc" {
		t.Error("a plain file changed")
	}
	if Detect(converttest.GPX(true)) != GPX || Detect([]byte("x")) != Unknown {
		t.Error("detection")
	}
	result, _ := Convert(converttest.GPX(true), typedef.SportInvalid)
	if Detect(result.FIT) != FIT {
		t.Error("a FIT file not detected")
	}
}

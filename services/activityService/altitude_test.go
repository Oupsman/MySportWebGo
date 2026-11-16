package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

// groundFunc is a terrain computed from the position
type groundFunc func(lat, lon float64) float64

func (g groundFunc) At(lat, lon float64) float64 { return g(lat, lon) }

// a slope rising 1 m every 0.0009° north (about 100 m) from 200 m at 45°N, unknown south of 44.5°N
var slope = groundFunc(func(lat, _ float64) float64 {
	if lat < 44.5 {
		return math.NaN()
	}
	return 200 + (lat-45)/0.0009
})

var nan = math.NaN()

func point(lat float64) types.GpsPoint { return types.GpsPoint{Lat: lat, Lon: 5} }

var noFix = types.GpsPoint{Lat: nan, Lon: nan}

func sameValues(got, want []float64) bool {
	return slices.EqualFunc(got, want, func(a, b float64) bool {
		return (math.IsNaN(a) && math.IsNaN(b)) || math.Abs(a-b) < 1e-6
	})
}

func TestCorrectAltitudes(t *testing.T) {
	// a barometer 30 m too high, and a bridge 15 m above the ground at the third point
	positions := []types.GpsPoint{noFix, point(45), point(45.0009), point(45.0018), point(45.0027), noFix}
	recorded := []float64{nan, 230, 231, 247, 233, 233}

	for _, c := range []struct {
		name, correction string
		ground           Ground
		positions        []types.GpsPoint
		recorded         []float64
		want             []float64
		applied          string
	}{
		{"none", models.AltitudeCorrectionNone, slope, positions, recorded, recorded, models.AltitudeCorrectionNone},
		{"without terrain", models.AltitudeCorrectionReplace, nil, positions, recorded, recorded, models.AltitudeCorrectionNone},
		{"unknown correction", "dem", slope, positions, recorded, recorded, models.AltitudeCorrectionNone},
		{"replace", models.AltitudeCorrectionReplace, slope, positions, recorded,
			[]float64{nan, 200, 201, 202, 203, nan}, models.AltitudeCorrectionReplace},
		// shifted by the median difference -30: the bridge doesn't move it
		{"calibrate", models.AltitudeCorrectionCalibrate, slope, positions, recorded,
			[]float64{nan, 200, 201, 217, 203, 203}, models.AltitudeCorrectionCalibrate},
		{"calibrate without barometer", models.AltitudeCorrectionCalibrate, slope, positions,
			[]float64{nan, nan, nan, nan, nan, nan}, []float64{nan, 200, 201, 202, 203, nan}, models.AltitudeCorrectionReplace},
		{"indoor", models.AltitudeCorrectionReplace, slope, []types.GpsPoint{noFix, noFix, noFix},
			[]float64{10, 11, 12}, []float64{10, 11, 12}, models.AltitudeCorrectionNone},
		// the ground of 1 position out of 3 known: the tiles are missing, the recorded altitudes stay
		{"terrain mostly unknown", models.AltitudeCorrectionReplace, slope,
			[]types.GpsPoint{point(44), point(44.1), point(45)}, []float64{10, 11, 12}, []float64{10, 11, 12},
			models.AltitudeCorrectionNone},
	} {
		got, applied := correctAltitudes(c.recorded, c.positions, c.correction, c.ground)
		if applied != c.applied || !sameValues(got, c.want) {
			t.Errorf("%s: %v %s, want %v %s", c.name, got, applied, c.want, c.applied)
		}
	}
}

func TestFillGaps(t *testing.T) {
	for _, c := range []struct{ values, want []float64 }{
		{[]float64{nan, 10, nan, nan, 16, nan}, []float64{10, 10, 12, 14, 16, 16}},
		{[]float64{1, 2, 3}, []float64{1, 2, 3}},
		{[]float64{nan, nan}, []float64{0, 0}},
		{[]float64{}, []float64{}},
	} {
		if got := fillGaps(c.values); !sameValues(got, c.want) {
			t.Errorf("fillGaps(%v) = %v, want %v", c.values, got, c.want)
		}
	}
}

// a ride north up the slope, a record every 10 s and ~100 m, its barometer recording altitude(i), NaN
// for none
func rideUpTheSlope(start time.Time, records int, altitude func(i int) float64) *filedef.Activity {
	fit := filedef.NewActivity()
	fit.Sessions = []*mesgdef.Session{mesgdef.NewSession(nil).SetStartTime(start).SetSport(typedef.SportCycling)}
	for i := 0; i < records; i++ {
		record := mesgdef.NewRecord(nil).SetTimestamp(start.Add(time.Duration(i) * 10 * time.Second)).
			SetDistance(uint32(i * 100 * 100)).SetEnhancedSpeedScaled(10).
			SetPositionLat(semicircles(45 + float64(i)*0.0009)).SetPositionLong(semicircles(5))
		if a := altitude(i); !math.IsNaN(a) {
			record.SetEnhancedAltitudeScaled(a)
		}
		fit.Records = append(fit.Records, record)
	}
	return fit
}

func TestAnalyzeRecordsCorrection(t *testing.T) {
	previous := Terrain
	Terrain = slope
	defer func() { Terrain = previous }()
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	const records = 100
	// a defective barometer: stuck at 500 m, jumping to 900 m every 10 records
	defective := func(i int) float64 {
		if i%10 == 5 {
			return 900
		}
		return 500
	}
	analyse := func(fit *filedef.Activity, correction string) models.Activity {
		t.Helper()
		activity := models.Activity{User: models.Users{SecurityDistance: 100}, Distance: records * 100, Sport: "cycling"}
		activity, err := AnalyzeRecords(fit, activity, correction)
		if err != nil {
			t.Fatal(err)
		}
		return activity
	}

	recorded := analyse(rideUpTheSlope(start, records, defective), models.AltitudeCorrectionNone)
	if recorded.ElevationCorrection != models.AltitudeCorrectionNone || recorded.Altitudes[0] != 500 {
		t.Errorf("without correction: %s, first altitude %v", recorded.ElevationCorrection, recorded.Altitudes[0])
	}

	replaced := analyse(rideUpTheSlope(start, records, defective), models.AltitudeCorrectionReplace)
	if replaced.ElevationCorrection != models.AltitudeCorrectionReplace {
		t.Fatalf("correction %s, want replace", replaced.ElevationCorrection)
	}
	// the last record isn't read by the analysis
	for i, altitude := range replaced.Altitudes {
		if math.Abs(altitude-float64(200+i)) > 1e-3 {
			t.Fatalf("altitude %d: %v, want the ground %d", i, altitude, 200+i)
		}
	}
	if math.Abs(replaced.PositiveElevation-float64(len(replaced.Altitudes)-2)) > 1e-3 || replaced.NegativeElevation != 0 {
		t.Errorf("elevation +%v -%v, want the climb of the slope", replaced.PositiveElevation, replaced.NegativeElevation)
	}
	// the estimated power climbs the slope: the same at every 10 m chunk, never the jumps of the barometer
	if len(replaced.Powers) == 0 || slices.Min(replaced.Powers) == 0 || slices.Max(replaced.Powers)-slices.Min(replaced.Powers) > 1 {
		t.Errorf("estimated powers %v, want a steady effort", replaced.Powers)
	}

	// a barometer 30 m too high: calibrated, the profile is kept
	high := func(i int) float64 { return float64(230 + i) }
	calibrated := analyse(rideUpTheSlope(start, records, high), models.AltitudeCorrectionCalibrate)
	if calibrated.ElevationCorrection != models.AltitudeCorrectionCalibrate ||
		math.Abs(calibrated.Altitudes[10]-210) > 1e-3 {
		t.Errorf("calibrated: %s, altitude %v at the 10th record, want 210", calibrated.ElevationCorrection,
			calibrated.Altitudes[10])
	}

	// records without altitude are interpolated, they were 0 m
	gaps := analyse(rideUpTheSlope(start, records, func(i int) float64 {
		if i >= 40 && i < 45 {
			return nan
		}
		return float64(200 + i)
	}), models.AltitudeCorrectionNone)
	for i := 40; i < 45; i++ {
		if math.Abs(gaps.Altitudes[i]-float64(200+i)) > 1e-3 {
			t.Errorf("altitude %d without record: %v, want %d", i, gaps.Altitudes[i], 200+i)
		}
	}
}

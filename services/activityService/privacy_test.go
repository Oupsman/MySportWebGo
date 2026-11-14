package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"math"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

func semicircles(degrees float64) int32 {
	return int32(math.Round(degrees * math.Pow(2, 31) / 180))
}

// a run going north from start, a point every ~100 m, whose first and last records have no GPS fix
func runWithoutFixAtEnds(start time.Time, points int) *filedef.Activity {
	fit := filedef.NewActivity()
	fit.Sessions = []*mesgdef.Session{mesgdef.NewSession(nil).SetStartTime(start).SetSport(typedef.SportRunning)}
	record := func(i int) *mesgdef.Record {
		return mesgdef.NewRecord(nil).SetTimestamp(start.Add(time.Duration(i) * 30 * time.Second)).
			SetDistance(uint32(i * 100 * 100))
	}
	fit.Records = append(fit.Records, record(0)) // no fix yet
	for i := 1; i <= points; i++ {
		fit.Records = append(fit.Records, record(i).
			SetPositionLat(semicircles(45+float64(i)*0.0009)).SetPositionLong(semicircles(5)))
	}
	fit.Records = append(fit.Records, record(points+1), record(points+2)) // saved after the GPS stopped
	return fit
}

func TestPrivacyZonesWithoutFixAtEnds(t *testing.T) {
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	fit := runWithoutFixAtEnds(start, 50)
	const securityDistance = 500 // meters
	activity := models.Activity{User: models.Users{SecurityDistance: securityDistance}, Distance: 5200}
	activity, err := AnalyzeRecords(fit, activity)
	if err != nil {
		t.Fatal(err)
	}
	first := types.GpsPoint{Lat: utils.SemiCircleToDegres(fit.Records[1].PositionLat), Lon: 5}
	last := types.GpsPoint{Lat: utils.SemiCircleToDegres(fit.Records[50].PositionLat), Lon: 5}
	if activity.StartPosition == nil || utils.Haversine(*activity.StartPosition, first) > 0.001 {
		t.Errorf("start position %v, want the first fix %v", activity.StartPosition, first)
	}
	if activity.EndPosition == nil || utils.Haversine(*activity.EndPosition, last) > 0.001 {
		t.Errorf("end position %v, want the last fix %v", activity.EndPosition, last)
	}
	if len(activity.PublicGpsPoints) == 0 {
		t.Fatal("no public point: the middle of the run is out of the zones")
	}
	for _, p := range activity.PublicGpsPoints {
		for name, private := range map[string]types.GpsPoint{"start": first, "end": last} {
			// Haversine is in km
			if d := utils.Haversine(p, private) * 1000; d <= securityDistance {
				t.Errorf("public point %v is %.0f m from the %s", p, d, name)
			}
		}
	}
}

func TestGpsRange(t *testing.T) {
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	fit := runWithoutFixAtEnds(start, 3)
	if first, last, found := gpsRange(fit.Records); !found || first != 1 || last != 3 {
		t.Errorf("gpsRange: %d %d %v, want 1 3 true", first, last, found)
	}
	if _, _, found := gpsRange(fit.Records[:1]); found {
		t.Error("gpsRange found a fix in a record without one")
	}
}

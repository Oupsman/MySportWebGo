package activityService

import (
	"MySportWeb/internal/pkg/models"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/rs/zerolog"
)

// writeRide writes a ride of a Garmin device to dir, with a Varia among its sensors when radar
func writeRide(t *testing.T, dir, name string, start time.Time, radar bool) string {
	t.Helper()
	fit := rideUpTheSlope(start, 100, func(int) float64 { return 500 })
	fit.Sessions[0].SetTotalDistance(99 * 100 * 100).SetTotalTimerTime(990 * 1000)
	fit.FileId = *mesgdef.NewFileId(nil).SetType(typedef.FileActivity).SetTimeCreated(start).
		SetManufacturer(typedef.ManufacturerGarmin).SetProductName("Edge Test").SetSerialNumber(1234)
	fit.DeviceInfos = append(fit.DeviceInfos, mesgdef.NewDeviceInfo(nil).SetTimestamp(start).
		SetDeviceIndex(typedef.DeviceIndexCreator).SetManufacturer(typedef.ManufacturerGarmin))
	if radar {
		// as an Edge lists a Varia RCT715
		fit.DeviceInfos = append(fit.DeviceInfos, mesgdef.NewDeviceInfo(nil).SetTimestamp(start).SetDeviceIndex(2).
			SetDeviceType(uint8(typedef.AntplusDeviceTypeBikeRadar)).SetManufacturer(typedef.ManufacturerGarmin).
			SetProduct(3808).SetSourceType(typedef.SourceTypeAntplus))
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	proto := fit.ToFIT(nil)
	if err := encoder.New(f).Encode(&proto); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRadarConnected(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	user := models.Users{SecurityDistance: 100}
	for _, radar := range []bool{true, false} {
		analysed, err := SumAnalyze(writeRide(t, dir, "ride.fit", start, radar), user, models.Equipments{})
		if err != nil {
			t.Fatal(err)
		}
		if analysed.RadarConnected == nil || *analysed.RadarConnected != radar {
			t.Errorf("radar %v: analysed as %v", radar, analysed.RadarConnected)
		}
	}
	// a Karoo doesn't list the radar: its events tell it
	fit := radarActivity(start, [][3]int{{1, 1, 40}}, -1)
	if !RadarConnected(fit, RadarEvents(fit, start)) {
		t.Error("radar events without a radar among the sensors: not connected")
	}
}

func TestBackfillRadarConnected(t *testing.T) {
	db := emptyDB(t)
	dir := t.TempDir()
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	user := models.Users{Username: "radar", Email: "radar@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	connected := true
	activities := map[string]*models.Activity{
		"with radar":    {FilePath: writeRide(t, dir, "radar.fit", start, true)},
		"without radar": {FilePath: writeRide(t, dir, "plain.fit", start, false)},
		"missing file":  {FilePath: filepath.Join(dir, "gone.fit")},
		// computed at its upload: kept
		"known": {FilePath: filepath.Join(dir, "gone.fit"), RadarConnected: &connected},
	}
	for _, a := range activities {
		a.UserID, a.Date, a.Sport = user.ID, start, "cycling"
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := BackfillRadarConnected(t.Context(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"with radar": true, "without radar": false, "missing file": false, "known": true}
	for name, a := range activities {
		var stored models.Activity
		if err := db.Select("radar_connected").First(&stored, "id = ?", a.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.RadarConnected == nil || *stored.RadarConnected != want[name] {
			t.Errorf("%s: radar connected %v, want %v", name, stored.RadarConnected, want[name])
		}
	}
	if ids, err := db.GetActivitiesWithoutRadarConnected(); err != nil || len(ids) != 0 {
		t.Errorf("left to do: %v %v", ids, err)
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/vars"
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/rs/zerolog"
)

// writeStuckRide writes the FIT file of a ride up the slope, recorded by a unit whose barometer is stuck at
// 500 m, and returns its path
func writeStuckRide(t *testing.T, dir, name string, start time.Time) string {
	t.Helper()
	fit := rideUpTheSlope(start, 100, func(int) float64 { return 500 })
	// 9.9 km in 990 s: without its distance, the analysis would stop after a km
	fit.Sessions[0].SetTotalDistance(99 * 100 * 100).SetTotalTimerTime(990 * 1000)
	fit.FileId = *mesgdef.NewFileId(nil).SetType(typedef.FileActivity).SetTimeCreated(start).
		SetManufacturer(typedef.ManufacturerGarmin).SetProductName("Edge Test").SetSerialNumber(1234)
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

// the re-analysis of an engine upgrade and the trimming correct the altitudes as the device asks
func TestCorrectionOfReanalysisAndTrim(t *testing.T) {
	db := emptyDB(t)
	dir := t.TempDir()
	// the thumbnails can't be drawn here (no tmp/ directory): only logged
	t.Chdir(dir)
	previous := Terrain
	Terrain = slope
	defer func() { Terrain = previous }()

	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	fitPath := writeStuckRide(t, dir, "ride.fit", start)

	user := models.Users{Username: "altitude", Email: "altitude@test.local", SecurityDistance: 100}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUser(uint64(user.ID))
	if err != nil {
		t.Fatal(err)
	}

	// as engine 5 saved it, before the user found the barometer defective
	analysed, err := SumAnalyze(fitPath, user, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	if analysed.DeviceName == "" || analysed.Device.Serial != "1234" || analysed.Altitudes[0] != 500 {
		t.Fatalf("device %q %+v, first altitude %v", analysed.DeviceName, analysed.Device, analysed.Altitudes[0])
	}
	deviceID, err := db.FindOrCreateDevice(user.ID, analysed.Device, analysed.DeviceName, models.AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAltitudeCorrection(user.ID, *deviceID, models.AltitudeCorrectionReplace); err != nil {
		t.Fatal(err)
	}
	if correction, err := db.DeviceAltitudeCorrection(user.ID, analysed.Device); err != nil ||
		correction != models.AltitudeCorrectionReplace {
		t.Fatalf("device correction %q, %v", correction, err)
	}
	other := analysed.Device
	other.Serial = "5678"
	if correction, err := db.DeviceAltitudeCorrection(user.ID, other); err != nil || correction != models.AltitudeCorrectionNone {
		t.Fatalf("correction of a unit never seen %q, %v", correction, err)
	}
	stored := analysed
	stored.ID = uuid.New()
	stored.UserID = user.ID
	stored.FilePath = fitPath
	stored.EngineVersion = 5
	stored.DeviceID = deviceID
	if err := db.Omit("User", "Equipment").Create(&stored).Error; err != nil {
		t.Fatal(err)
	}

	check := func(when string, wantRecords int) *models.Activity {
		t.Helper()
		got, err := db.GetActivity(stored.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ElevationCorrection != models.AltitudeCorrectionReplace || got.DeviceID == nil || *got.DeviceID != *deviceID {
			t.Errorf("%s: correction %q, device %v", when, got.ElevationCorrection, got.DeviceID)
		}
		if len(got.Altitudes) != wantRecords {
			t.Fatalf("%s: %d altitudes, want %d", when, len(got.Altitudes), wantRecords)
		}
		for i, altitude := range got.Altitudes {
			if math.Abs(altitude-float64(200+i)) > 1e-3 {
				t.Fatalf("%s: altitude %d: %v, want the ground %d", when, i, altitude, 200+i)
			}
		}
		if math.Abs(got.PositiveElevation-float64(wantRecords-2)) > 1e-3 {
			t.Errorf("%s: elevation +%v, want %d", when, got.PositiveElevation, wantRecords-2)
		}
		return got
	}

	if err := BackfillTimeStamps(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got := check("re-analysed", len(analysed.Altitudes))
	if got.EngineVersion != vars.Engine_version {
		t.Errorf("engine %d", got.EngineVersion)
	}

	// trimmed after 500 s: still corrected, still on its device
	if _, err := TrimActivity(db, got, 500, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	trimmed, err := db.GetActivity(stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	check("trimmed", len(trimmed.Altitudes))
	if len(trimmed.Altitudes) >= len(analysed.Altitudes) {
		t.Errorf("%d altitudes after trimming, %d before", len(trimmed.Altitudes), len(analysed.Altitudes))
	}
}

func TestRecompute(t *testing.T) {
	db := emptyDB(t)
	dir := t.TempDir()
	previous := Terrain
	Terrain = slope
	defer func() { Terrain = previous }()

	user := models.Users{Username: "recompute", Email: "recompute@test.local", SecurityDistance: 100}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUser(uint64(user.ID))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	// imported before the user set the correction: the altitudes of the stuck barometer
	save := func(path string, daysLater int, deviceID *uuid.UUID) models.Activity {
		t.Helper()
		analysed, err := SumAnalyze(writeStuckRide(t, dir, "source.fit", start), user, models.Equipments{})
		if err != nil {
			t.Fatal(err)
		}
		analysed.ID = uuid.New()
		analysed.UserID = user.ID
		analysed.FilePath = path
		analysed.Date = start.AddDate(0, 0, daysLater)
		analysed.DeviceID = deviceID
		if err := db.Omit("User", "Equipment").Create(&analysed).Error; err != nil {
			t.Fatal(err)
		}
		return analysed
	}
	reference, err := SumAnalyze(writeStuckRide(t, dir, "source.fit", start), user, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	deviceID, err := db.FindOrCreateDevice(user.ID, reference.Device, reference.DeviceName, models.AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	otherDevice, err := db.FindOrCreateDevice(user.ID, types.Device{Brand: "Garmin", Model: "Fenix 6X"}, "Garmin Fenix 6X",
		models.AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	first := save(writeStuckRide(t, dir, "first.fit", start), 0, deviceID)
	second := save(writeStuckRide(t, dir, "second.fit", start), 1, deviceID)
	lost := save(filepath.Join(dir, "deleted.fit"), 2, deviceID)
	untouched := save(writeStuckRide(t, dir, "fenix.fit", start), 3, otherDevice)
	if err := db.SetAltitudeCorrection(user.ID, *deviceID, models.AltitudeCorrectionReplace); err != nil {
		t.Fatal(err)
	}

	// every analysis slot taken by uploads: the recompute waits for one
	saved := BusyRetryDelay
	BusyRetryDelay = 10 * time.Millisecond
	defer func() { BusyRetryDelay = saved }()
	var releases []func()
	for range MaxConcurrentAnalyses {
		release, err := analysisSlots.acquire(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	status, err := StartRecompute(testBackground{}, db, user.ID, *deviceID, zerolog.Nop())
	if err != nil || !status.Running || status.Total != 3 {
		t.Fatalf("started: %+v, %v", status, err)
	}
	if _, err := StartRecompute(testBackground{}, db, user.ID, *deviceID, zerolog.Nop()); err != ErrRecomputeRunning {
		t.Errorf("second recompute while running: %v", err)
	}
	for _, release := range releases {
		release()
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		status := RecomputeStatusOf(*deviceID)
		if !status.Running {
			if status.Done != 2 || status.Failed != 1 || status.FinishedAt == nil {
				t.Errorf("finished: %+v", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still running: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if RecomputeStatusOf(*otherDevice) != nil {
		t.Error("a recompute status for the other device")
	}

	for _, activity := range []models.Activity{first, second} {
		got, err := db.GetActivity(activity.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ElevationCorrection != models.AltitudeCorrectionReplace || math.Abs(got.Altitudes[10]-210) > 1e-3 ||
			math.Abs(got.PositiveElevation-float64(len(got.Altitudes)-2)) > 1e-3 {
			t.Errorf("%s: correction %q, altitude %v, elevation %v", activity.ID, got.ElevationCorrection,
				got.Altitudes[10], got.PositiveElevation)
		}
		// what the user set is kept
		if got.Title != activity.Title || !got.Date.Equal(activity.Date) {
			t.Errorf("%s: title %q date %v changed", activity.ID, got.Title, got.Date)
		}
	}
	for name, activity := range map[string]models.Activity{"missing file": lost, "other device": untouched} {
		got, err := db.GetActivity(activity.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ElevationCorrection != models.AltitudeCorrectionNone || got.Altitudes[10] != 500 {
			t.Errorf("%s: correction %q, altitude %v", name, got.ElevationCorrection, got.Altitudes[10])
		}
	}

	// done: it can start again
	if _, err := StartRecompute(testBackground{}, db, user.ID, *deviceID, zerolog.Nop()); err != nil {
		t.Errorf("recompute after the first one: %v", err)
	}
	for RecomputeStatusOf(*deviceID).Running {
		time.Sleep(10 * time.Millisecond)
	}
}

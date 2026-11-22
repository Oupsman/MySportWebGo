package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// saveRide saves a ride of an hour at watts, its best 20 minutes best20, at a date, without power metrics
func saveRide(t *testing.T, db *models.DB, user models.Users, sport string, date time.Time, watts uint16,
	best20 float64) models.Activity {
	t.Helper()
	a := rideWithPower(watts, 3600)
	a.ID, a.UserID, a.Sport, a.Date, a.Title = uuid.New(), user.ID, sport, date, "ride"
	a.BestEfforts = types.BestEffortArray{{Sport: sport, Kind: types.EffortPower, Target: FTPDuration, Value: best20}}
	trimp := 42.0
	a.Trimp, a.ScoreSource = &trimp, models.ScoreHR
	if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Activity{}).Where("id = ?", a.ID).Update("power_zones", nil).Error; err != nil {
		t.Fatal(err)
	}
	return a
}

func storedPower(t *testing.T, db *models.DB, id uuid.UUID) models.Activity {
	t.Helper()
	var a models.Activity
	if err := db.First(&a, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return a
}

func TestUserFTP(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "ftp", Email: "ftp@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	saveRide(t, db, user, "cycling", day.AddDate(0, 0, -50), 200, 400) // too old
	saveRide(t, db, user, "cycling", day.AddDate(0, 0, -20), 200, 260)
	saveRide(t, db, user, "running", day.AddDate(0, 0, -10), 200, 350) // a run's power doesn't count
	saveRide(t, db, user, "cycling", day.AddDate(0, 0, 1), 200, 380)   // after the date

	ftp := UserFTP(db, user.ID)
	check := func(own float64, want uint16) {
		t.Helper()
		got, err := ftp(day, own)
		if err != nil || got != want {
			t.Errorf("own %v: FTP %d, want %d (%v)", own, got, want, err)
		}
	}
	check(0, EstimateFTP(260))   // 247
	check(300, EstimateFTP(300)) // the activity analysed counts
	if got, _ := ftp(day.AddDate(0, 0, -60), 0); got != 0 {
		t.Errorf("no ride before: %d", got)
	}
	// set by the user: theirs, whatever the rides
	if err := db.Model(&user).Update("ftp", 280).Error; err != nil {
		t.Fatal(err)
	}
	check(300, 280)
}

func TestBackfillPowerMetrics(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "power", Email: "power@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	ride := saveRide(t, db, user, "cycling", day, 250, 263) // FTP 250
	device := saveRide(t, db, user, "cycling", day.AddDate(0, 0, 1), 250, 263)
	if err := db.Model(&device).Update("score_source", models.ScoreDevice).Error; err != nil {
		t.Fatal(err)
	}
	run := saveRide(t, db, user, "running", day.AddDate(0, 0, 2), 250, 263)

	if err := BackfillPowerMetrics(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got := storedPower(t, db, ride.ID)
	if got.FTP != 250 || got.TSS == nil || *got.TSS != 100 || got.Intensity == nil || *got.Intensity != 1 ||
		len(got.PowerZones) != models.PowerZones || got.PowerZones[3] != 3600 {
		t.Fatalf("ride: ftp %d, tss %v, if %v, zones %v", got.FTP, got.TSS, got.Intensity, got.PowerZones)
	}
	// the TSS becomes the training score, the device's stays
	if *got.Trimp != 100 || got.ScoreSource != models.ScorePower {
		t.Errorf("ride score %v from %s", *got.Trimp, got.ScoreSource)
	}
	if d := storedPower(t, db, device.ID); *d.Trimp != 42 || d.ScoreSource != models.ScoreDevice || d.TSS == nil {
		t.Errorf("device score %v from %s, tss %v", *d.Trimp, d.ScoreSource, d.TSS)
	}
	if r := storedPower(t, db, run.ID); r.TSS != nil || r.PowerZones == nil || *r.Trimp != 42 {
		t.Errorf("run: tss %v zones %v", r.TSS, r.PowerZones)
	}
	// nothing left
	if ids, _ := db.GetActivitiesWithoutPowerZones(); len(ids) != 0 {
		t.Errorf("%d left", len(ids))
	}

	// the weekly zones
	weeks, err := db.GetWeeklyPowerZones(user.ID, day.AddDate(0, 0, 1), 1) // the week of the rides
	if err != nil || len(weeks) != 1 || weeks[0].Zones[3] != 7200 {
		t.Errorf("weeks %v %v", weeks, err)
	}

	// an FTP the user sets: everything again
	if err := db.Model(&user).Update("ftp", 200).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.ResetPowerMetrics(user.ID); err != nil {
		t.Fatal(err)
	}
	if err := BackfillPowerMetrics(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if got := storedPower(t, db, ride.ID); got.FTP != 200 || *got.Trimp != 156.2 {
		t.Errorf("FTP 200: ftp %d, score %v", got.FTP, *got.Trimp)
	}
}

func TestPowerScoreBackToTrimp(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "short", Email: "short@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	// a ride scored by its power, whose best 20 minutes are gone (re-analysed shorter): no FTP anymore
	ride := saveRide(t, db, user, "cycling", time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC), 250, 0)
	if err := db.Model(&ride).Updates(map[string]interface{}{"score_source": models.ScorePower, "trimp": 100}).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillPowerMetrics(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	// its TRIMP: no heart rate, 0
	if got := storedPower(t, db, ride.ID); got.ScoreSource != models.ScoreHR || *got.Trimp != 0 || got.TSS != nil {
		t.Errorf("score %v from %s, tss %v", *got.Trimp, got.ScoreSource, got.TSS)
	}
}

func TestPowerRideChanged(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "order", Email: "order@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	later := saveRide(t, db, user, "cycling", day.AddDate(0, 0, 10), 200, 200)   // FTP 190 from itself
	tooLate := saveRide(t, db, user, "cycling", day.AddDate(0, 0, 50), 200, 200) // out of the window
	sync := func(fn func(ctx context.Context)) { fn(context.Background()) }
	if err := BackfillPowerMetrics(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if got := storedPower(t, db, later.ID); got.FTP != 190 {
		t.Fatalf("before: FTP %d", got.FTP)
	}

	// an older ride imported after them, stronger: the rides of the 42 days after it get its FTP
	older := saveRide(t, db, user, "cycling", day, 300, 300)
	older.ScoreSource = ""
	if err := BackfillPowerMetrics(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	PowerRideChanged(db, older, true, sync, zerolog.Nop())
	if got := storedPower(t, db, later.ID); got.FTP != 285 {
		t.Errorf("after the import: FTP %d, want 285", got.FTP)
	}
	if got := storedPower(t, db, tooLate.ID); got.FTP != 190 {
		t.Errorf("out of the window: FTP %d", got.FTP)
	}

	// deleted: back to their own
	if err := db.Delete(&models.Activity{}, "id = ?", older.ID).Error; err != nil {
		t.Fatal(err)
	}
	PowerRideChanged(db, older, false, sync, zerolog.Nop())
	if got := storedPower(t, db, later.ID); got.FTP != 190 {
		t.Errorf("after the delete: FTP %d, want 190", got.FTP)
	}
}

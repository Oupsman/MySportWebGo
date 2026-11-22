package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// activities imported by engine 1 get the time stamps of their sparse series from their FIT file
func TestBackfillTimeStamps(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "stamps", Email: "stamps@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID)}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}

	// imported before engine 2: no time stamps
	old := importFit(t, sameRouteFiles[0], user, equipment)
	old.FilePath = "../../prototypes/" + sameRouteFiles[0] + ".fit"
	want := old.GpsTimeStamps
	old.EngineVersion = 1
	old.GpsTimeStamps, old.HeartTimeStamps, old.CadenceTimeStamps = nil, nil, nil
	// its FIT file was deleted: marked as done, without time stamps
	lost := importFit(t, sameRouteFiles[1], user, equipment)
	lost.FilePath = "../../prototypes/missing.fit"
	lost.EngineVersion = 1
	lost.GpsTimeStamps = nil
	for _, a := range []*models.Activity{&old, &lost} {
		if err := db.CreateActivity(a); err != nil {
			t.Fatal(err)
		}
	}

	if err := BackfillTimeStamps(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetActivity(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EngineVersion != vars.Engine_version || len(got.GpsTimeStamps) != len(want) || len(got.GpsTimeStamps) != len(got.GpsPoints) {
		t.Fatalf("not re-analysed: engine %d, %d gps time stamps for %d points (want %d)",
			got.EngineVersion, len(got.GpsTimeStamps), len(got.GpsPoints), len(want))
	}
	got, err = db.GetActivity(lost.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EngineVersion != vars.Engine_version || len(got.GpsTimeStamps) != 0 {
		t.Fatalf("activity without FIT file: engine %d, %d gps time stamps", got.EngineVersion, len(got.GpsTimeStamps))
	}
	// nothing left to do
	ids, err := db.GetActivitiesBelowEngine(vars.Engine_version)
	if err != nil || len(ids) != 0 {
		t.Fatalf("activities still below engine %d: %v %v", vars.Engine_version, ids, err)
	}
}

// while the uploads take every analysis slot, the backfill waits for one instead of taking the refused
// analysis for an unreadable file; stopped while waiting, it leaves the activity for the next start
func TestBackfillTimeStampsWaitsWhenBusy(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "stamps-busy", Email: "stamps-busy@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID)}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}
	old := importFit(t, sameRouteFiles[0], user, equipment)
	old.FilePath = "../../prototypes/" + sameRouteFiles[0] + ".fit"
	want := len(old.GpsTimeStamps)
	old.EngineVersion = 1
	old.GpsTimeStamps, old.HeartTimeStamps, old.CadenceTimeStamps = nil, nil, nil
	if err := db.CreateActivity(&old); err != nil {
		t.Fatal(err)
	}
	engine := func() int {
		got, err := db.GetActivity(old.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.EngineVersion
	}

	release := fillAnalysisSlots(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := BackfillTimeStamps(ctx, db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if got := engine(); got != 1 {
		t.Fatalf("stopped while busy: engine %d, want 1 (left for the next start)", got)
	}

	time.AfterFunc(100*time.Millisecond, release)
	if err := BackfillTimeStamps(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetActivity(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EngineVersion != vars.Engine_version || len(got.GpsTimeStamps) != want {
		t.Fatalf("after the wait: engine %d, %d gps time stamps (want %d): degraded instead of re-analysed",
			got.EngineVersion, len(got.GpsTimeStamps), want)
	}
}

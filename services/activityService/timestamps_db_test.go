package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"testing"

	"github.com/rs/zerolog"
)

// activities imported by engine 1 get the time stamps of their sparse series from their FIT file
func TestBackfillTimeStamps(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "stamps", Email: "stamps@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID), IsDefault: true}
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

	if err := BackfillTimeStamps(db, zerolog.Nop()); err != nil {
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

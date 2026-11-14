package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
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

// the activities and routes saved by the engines before 5, whose privacy zones were around the first and
// the last records even without GPS fix, are fixed by the re-analysis
func TestBackfillFixesPrivacyZones(t *testing.T) {
	db := emptyDB(t)
	dir := t.TempDir()
	// the thumbnails can't be drawn here (no tmp/ directory): only logged
	t.Chdir(dir)

	start := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	fit := runWithoutFixAtEnds(start, 50)
	fit.FileId = *mesgdef.NewFileId(nil).SetType(typedef.FileActivity).SetTimeCreated(start)
	fitPath := filepath.Join(dir, "run.fit")
	f, err := os.Create(fitPath)
	if err != nil {
		t.Fatal(err)
	}
	proto := fit.ToFIT(nil)
	if err := encoder.New(f).Encode(&proto); err != nil {
		t.Fatal(err)
	}
	f.Close()

	user := models.Users{Username: "privacy", Email: "privacy@test.local", SecurityDistance: 500}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	user, err = db.GetUser(uint64(user.ID))
	if err != nil {
		t.Fatal(err)
	}

	// as engine 4 saved it: endpoints and route reference read from the records without fix, every
	// point public
	analysed, err := SumAnalyze(fitPath, user, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	bogus := types.GpsPoint{Lat: utils.SemiCircleToDegres(1<<31 - 1), Lon: utils.SemiCircleToDegres(1<<31 - 1)}
	route := models.Route{UserID: user.ID, Sport: analysed.Sport, Distance: analysed.Distance, ActivityCount: 1,
		StartPosition: &bogus, EndPosition: &bogus, SimplifiedGpsPoints: analysed.SimplifiedGpsPoints}
	if err := db.CreateRoute(&route); err != nil {
		t.Fatal(err)
	}
	old := analysed
	old.ID = uuid.New()
	old.FilePath = fitPath
	old.EngineVersion = 4
	old.StartPosition, old.EndPosition = &bogus, &bogus
	old.PublicGpsPoints = analysed.GpsPoints
	old.RouteID = &route.ID

	// its FIT file was deleted: the public trace is computed again from the saved one
	lost := analysed
	lost.ID = uuid.New()
	lost.Date = start.Add(time.Hour)
	lost.FilePath = filepath.Join(dir, "missing.fit")
	lost.EngineVersion = 4
	lost.PublicGpsPoints = analysed.GpsPoints
	for _, a := range []*models.Activity{&old, &lost} {
		a.UserID = user.ID
		if err := db.Omit("User", "Equipment").Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := BackfillTimeStamps(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}

	gps := analysed.GpsPoints
	first, last := gps[0], gps[len(gps)-1]
	for _, id := range []uuid.UUID{old.ID, lost.ID} {
		got, err := db.GetActivity(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.EngineVersion != vars.Engine_version {
			t.Errorf("%s: engine %d", id, got.EngineVersion)
		}
		if len(got.PublicGpsPoints) == 0 || len(got.PublicGpsPoints) >= len(gps) {
			t.Errorf("%s: %d public points of %d", id, len(got.PublicGpsPoints), len(gps))
		}
		for _, p := range got.PublicGpsPoints {
			if utils.Haversine(p, first)*1000 <= 500 || utils.Haversine(p, last)*1000 <= 500 {
				t.Errorf("%s: public point %v in a privacy zone", id, p)
			}
		}
		if id == old.ID && (!ValidPosition(got.StartPosition) || !ValidPosition(got.EndPosition)) {
			t.Errorf("start %v end %v still invalid", got.StartPosition, got.EndPosition)
		}
	}
	fixed, err := db.GetRoute(route.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidPosition(fixed.StartPosition) || !ValidPosition(fixed.EndPosition) {
		t.Errorf("route endpoints %v %v still invalid", fixed.StartPosition, fixed.EndPosition)
	}
}

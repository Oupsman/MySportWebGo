package activityService

import (
	"MySportWeb/internal/pkg/models"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// TestRiderWeightQueries: the weight of the analysis (SQL) and the one of the backfill (in memory) agree
func TestRiderWeightQueries(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "weights", Email: "weights@test.local", Weight: 66}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	day := func(d int) time.Time { return time.Date(2026, 3, d, 8, 0, 0, 0, time.UTC) }
	dates := []time.Time{day(1), day(10), day(15), day(20), day(25)}
	check := func(when string) {
		t.Helper()
		measures, err := db.GetWeightMeasures(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, date := range dates {
			sql, err := db.GetRiderWeight(user.ID, date)
			if err != nil {
				t.Fatal(err)
			}
			if memory := riderWeightAt(measures, 66, date); sql != memory {
				t.Errorf("%s, %v: %v kg in SQL, %v kg in memory", when, date, sql, memory)
			}
		}
	}
	check("without measure")
	for _, m := range []models.HealthData{
		{UserID: user.ID, Date: day(10), Weight: 80},
		{UserID: user.ID, Date: day(20), Weight: 78},
		{UserID: user.ID, Date: day(15), RestHR: 50}, // without weight
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	check("with measures")
	if w, _ := db.GetRiderWeight(user.ID, day(15)); w != 80 {
		t.Errorf("weight on the 15th: %v kg, want 80 (the 10th)", w)
	}
}

// TestBackfillWeights analyses again the rides of before the weights (110 kg), with the weight of their rider
// and bike; the other sports only get their weight; a second run has nothing to do
func TestBackfillWeights(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "weights", Email: "weights@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	bike := models.Equipments{UserID: int(user.ID), Name: "bike", Weight: 9}
	if err := db.Create(&bike).Error; err != nil {
		t.Fatal(err)
	}
	ride := importFit(t, sameRouteFiles[0], user, models.Equipments{})
	if ride.Sport != "cycling" {
		t.Skipf("%s is a %s", sameRouteFiles[0], ride.Sport)
	}
	if err := db.Create(&models.HealthData{UserID: user.ID, Date: ride.Date.AddDate(0, 0, -3), Weight: 74}).Error; err != nil {
		t.Fatal(err)
	}
	// as analysed before: 110 kg
	old, err := AnalyzeTrimmed("../../prototypes/"+sameRouteFiles[0]+".fit", user, 0,
		Analysis{Weight: func(time.Time, string) (float64, error) { return 110, nil }})
	if err != nil {
		t.Fatal(err)
	}
	ride.ID = uuid.New()
	ride.FilePath, _ = filepath.Abs("../../prototypes/" + sameRouteFiles[0] + ".fit")
	ride.UserID, ride.EquipmentID = user.ID, &bike.ID
	ride.TotalWeight, ride.Powers, ride.AvgPower = old.TotalWeight, old.Powers, old.AvgPower
	run := models.Activity{ID: uuid.New(), UserID: user.ID, Sport: "running", Date: ride.Date, TotalWeight: 110}
	for _, a := range []*models.Activity{&ride, &run} {
		if err := db.Omit("User", "Equipment").Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}

	// stopped before starting: nothing changes
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if err := BackfillWeights(stopped, db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	var got models.Activity
	if err := db.First(&got, "id = ?", ride.ID).Error; err != nil || got.TotalWeight != 110 {
		t.Fatalf("after a stopped backfill: %d kg (%v), want still 110", got.TotalWeight, err)
	}

	if err := BackfillWeights(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&got, "id = ?", ride.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.TotalWeight != 74+9 {
		t.Errorf("ride: %d kg, want 83 (74 measured + 9 of the bike)", got.TotalWeight)
	}
	if got.AvgPower == 0 || got.AvgPower >= old.AvgPower {
		t.Errorf("ride: %d W at 83 kg, want below %d W at 110 kg", got.AvgPower, old.AvgPower)
	}
	var gotRun models.Activity
	if err := db.First(&gotRun, "id = ?", run.ID).Error; err != nil || gotRun.TotalWeight != 74 {
		t.Errorf("run: %d kg (%v), want 74 without bike", gotRun.TotalWeight, err)
	}

	var buf bytes.Buffer
	if err := BackfillWeights(context.Background(), db, zerolog.New(&buf)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "analysing again") {
		t.Errorf("second run analysed again: %s", buf.String())
	}

	// a new weight measured before the ride: analysed again at the next run
	if err := db.Create(&models.HealthData{UserID: user.ID, Date: ride.Date.Add(-time.Hour), Weight: 71}).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillWeights(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got = models.Activity{}
	if err := db.First(&got, "id = ?", ride.ID).Error; err != nil || got.TotalWeight != 71+9 {
		t.Errorf("ride after a new measure: %d kg (%v), want 80", got.TotalWeight, err)
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestBackfillTrimp(t *testing.T) {
	db := emptyDB(t)
	if err := db.Exec("TRUNCATE health_data RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := models.Users{Username: "trimp", Email: "trimp@test.local", MaxHR: 190}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateHealthData(&models.HealthData{UserID: user.ID, Date: trimpDate.Add(-24 * time.Hour), RestHR: 80}); err != nil {
		t.Fatal(err)
	}
	add := func(a models.Activity) models.Activity {
		a.User, a.UserID, a.Sport = models.Users{}, user.ID, "running"
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a
	}
	steady := add(steadyActivity(user, 60, 125))
	noHR := add(models.Activity{Date: trimpDate.Add(time.Hour)})
	done := 12.5
	computed := add(models.Activity{Date: trimpDate.Add(2 * time.Hour), Trimp: &done})

	if err := BackfillTrimp(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	// with the measured resting heart rate (80), not the default one (60): below 50.1
	want := BanisterTRIMP(steadyActivity(user, 60, 125), 80)
	for _, c := range []struct {
		activity models.Activity
		want     float64
	}{{steady, want}, {noHR, 0}, {computed, 12.5}} {
		var trimps []*float64
		if err := db.Model(&models.Activity{}).Where("id = ?", c.activity.ID).Pluck("trimp", &trimps).Error; err != nil {
			t.Fatal(err)
		}
		if len(trimps) != 1 || trimps[0] == nil || *trimps[0] != c.want {
			t.Errorf("activity of %s: TRIMP %v, want %v", c.activity.Date, trimps, c.want)
		}
	}
	if ids, err := db.GetActivitiesWithoutTrimp(); err != nil || len(ids) != 0 {
		t.Errorf("%d activities still without TRIMP (%v)", len(ids), err)
	}
}

// a backfill stopped while the server is busy saves nothing: the file wasn't read, the TRIMP isn't computed
// from the heart rates instead
func TestBackfillTrimpStoppedWhileBusy(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "busy", Email: "busy@test.local", MaxHR: 190}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	activity := steadyActivity(user, 60, 125)
	activity.User, activity.UserID, activity.Sport = models.Users{}, user.ID, "running"
	if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
		t.Fatal(err)
	}

	fillAnalysisSlots(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := BackfillTrimp(ctx, db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if ids, err := db.GetActivitiesWithoutTrimp(); err != nil || len(ids) != 1 {
		t.Errorf("%d activities without TRIMP, want 1 (%v)", len(ids), err)
	}
}

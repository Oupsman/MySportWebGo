package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestBackfillHrZones(t *testing.T) {
	db := emptyDB(t)
	if err := db.Exec("TRUNCATE health_data RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := models.Users{Username: "zones", Email: "zones@test.local", MaxHR: 190}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	// measured resting heart rate 80: reserve 110, zone 2 from 146 bpm (it's 138 with the default 60)
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
	steady := add(steadyActivity(user, 60, 140))
	noHR := add(models.Activity{Date: trimpDate.Add(time.Hour)})
	done := steadyActivity(user, 60, 140)
	done.Date, done.HrZones = trimpDate.Add(2*time.Hour), types.FloatArray{1, 2, 3, 4, 5}
	done = add(done)

	if err := BackfillHrZones(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	check := func(activity models.Activity, zones types.FloatArray, bounds types.Uint16Array) {
		t.Helper()
		var stored models.Activity
		if err := db.Select("hr_zones", "hr_zone_bounds").First(&stored, "id = ?", activity.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(stored.HrZones, zones) || !reflect.DeepEqual(stored.HrZoneBounds, bounds) {
			t.Errorf("activity of %s: zones %v bounds %v, want %v %v", activity.Date, stored.HrZones,
				stored.HrZoneBounds, zones, bounds)
		}
	}
	check(steady, types.FloatArray{3600, 0, 0, 0, 0}, types.Uint16Array{80, 146, 157, 168, 179, 190})
	check(noHR, types.FloatArray{0, 0, 0, 0, 0}, nil)
	check(done, types.FloatArray{1, 2, 3, 4, 5}, nil)
	if ids, err := db.GetActivitiesWithoutHrZones(); err != nil || len(ids) != 0 {
		t.Errorf("%d activities still without zones (%v)", len(ids), err)
	}

	// a lower maximum: the reset activities are computed again with it, 140 bpm is now in zone 3
	if err := db.UpdateUserFields(user.ID, map[string]interface{}{"max_hr": 160}); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetHrZones(user.ID); err != nil {
		t.Fatal(err)
	}
	if err := BackfillHrZones(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	check(steady, types.FloatArray{0, 0, 3600, 0, 0}, types.Uint16Array{80, 128, 136, 144, 152, 160})
}

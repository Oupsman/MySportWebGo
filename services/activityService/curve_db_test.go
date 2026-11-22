package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestUserPowerCurve(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "curve", Email: "curve@test.local", Weight: 70}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	// an hour at 250 W, then 30 min at 300 W a month later, the same 30 min at 300 W later on (a tie),
	// and a run at 400 W
	hour := saveRide(t, db, user, "cycling", day, 250, 250)
	half := saveRide(t, db, user, "cycling", day.AddDate(0, 1, 0), 300, 300)
	half.Powers, half.PowerTS = half.Powers[:1801], half.PowerTS[:1801]
	if err := db.Model(&half).Updates(map[string]interface{}{"powers": half.Powers, "power_ts": half.PowerTS}).Error; err != nil {
		t.Fatal(err)
	}
	tie := saveRide(t, db, user, "cycling", day.AddDate(0, 2, 0), 300, 300)
	tie.Powers, tie.PowerTS = tie.Powers[:1801], tie.PowerTS[:1801]
	if err := db.Model(&tie).Updates(map[string]interface{}{"powers": tie.Powers, "power_ts": tie.PowerTS}).Error; err != nil {
		t.Fatal(err)
	}
	saveRide(t, db, user, "running", day.AddDate(0, 1, 1), 400, 400)
	if err := db.Model(&models.Activity{}).Where("user_id = ?", user.ID).Update("power_curve", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillPowerCurves(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if ids, _ := db.GetActivitiesWithoutPowerCurve(); len(ids) != 0 {
		t.Fatalf("%d left", len(ids))
	}
	// 75 kg measured before the second ride
	if err := db.Create(&models.HealthData{UserID: user.ID, Date: day.AddDate(0, 0, 15), Weight: 75}).Error; err != nil {
		t.Fatal(err)
	}

	byDuration := func(from, to *time.Time) map[float64]PowerCurveBest {
		t.Helper()
		curve, _, err := UserPowerCurve(db, user, from, to)
		if err != nil {
			t.Fatal(err)
		}
		points := map[float64]PowerCurveBest{}
		for _, p := range curve {
			points[p.Duration] = p
		}
		return points
	}
	all := byDuration(nil, nil)
	// up to 30 min: the 300 W of the oldest of the two half hours; beyond: the hour; never the run's 400 W
	if p := all[1800]; p.Watts != 300 || p.ActivityID != half.ID || p.ActivityTitle != "ride" {
		t.Errorf("30 min: %+v", p)
	}
	if p := all[3600]; p.Watts != 250 || p.ActivityID != hour.ID {
		t.Errorf("1 h: %+v", p)
	}
	if _, ok := all[4500]; ok {
		t.Error("a point longer than every ride")
	}
	// the weight at the date of the ride the point comes from: 75 kg, measured before it
	if p := all[1800]; p.WattsPerKg == nil || *p.WattsPerKg != 4 {
		t.Errorf("30 min W/kg %v", p.WattsPerKg)
	}

	// a period: from included, to excluded
	from, to := day, day.AddDate(0, 1, 0)
	first := byDuration(&from, &to)
	if p := first[1800]; p.Watts != 250 || p.ActivityID != hour.ID {
		t.Errorf("first month, 30 min: %+v", p)
	}
	after := day.AddDate(0, 1, 1)
	if p := byDuration(&after, nil)[1800]; p.ActivityID != tie.ID {
		t.Errorf("since the second ride: %+v", p)
	}
}

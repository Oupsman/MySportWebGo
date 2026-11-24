package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"testing"

	"github.com/rs/zerolog"
)

// the moving time of real rides: at most their duration, the same from the analysis and from the stored series
func TestMovingTimeOfRides(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "moving", Email: "moving@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	analysed := map[string]float64{}
	for _, name := range sameRouteFiles {
		a := importFit(t, name, user, models.Equipments{})
		if a.MovingTime == nil || a.AvgMovingSpeed == nil {
			t.Fatalf("%s: no moving time", name)
		}
		moving, speed := *a.MovingTime, *a.AvgMovingSpeed
		if moving <= 0 || moving > float64(a.Duration) || speed < a.AvgSpeed*0.99 {
			t.Errorf("%s: moving %v s of %d, at %.2f m/s (average %.2f)", name, moving, a.Duration, speed, a.AvgSpeed)
		}
		t.Logf("%s: %d s, moving %.0f s; %.1f km/h, moving %.1f km/h", name, a.Duration, moving, a.AvgSpeed*3.6, speed*3.6)
		if err := db.CreateActivity(&a); err != nil {
			t.Fatal(err)
		}
		analysed[a.ID.String()] = moving
	}

	// imported before: computed from their stored series, as by the analysis
	if err := db.Exec("UPDATE activities SET moving_time = NULL, avg_moving_speed = NULL").Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillMovingTime(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	var rows []models.Activity
	db.Select("id", "moving_time", "avg_moving_speed", "title").Find(&rows)
	for _, a := range rows {
		if a.MovingTime == nil || *a.MovingTime != analysed[a.ID.String()] {
			t.Errorf("%s: backfilled %v, analysed %v", a.ID, a.MovingTime, analysed[a.ID.String()])
		}
	}
	if left, _ := db.GetActivitiesWithoutMovingTime(); len(left) != 0 {
		t.Errorf("%d left without moving time", len(left))
	}

	// an edit of the activity keeps it
	a, err := db.GetActivity(rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	a.Title = "edited"
	if err := db.UpdateActivity(a); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.GetActivity(a.ID); again.MovingTime == nil || *again.MovingTime != analysed[a.ID.String()] {
		t.Errorf("after an edit: %v", again.MovingTime)
	}
}

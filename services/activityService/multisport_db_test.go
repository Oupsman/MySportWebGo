package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"testing"
	"time"
)

func TestLegsStored(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "legs", Email: "legs@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 6, 14, 7, 0, 0, 0, time.UTC)
	triathlon := models.Activity{UserID: user.ID, Date: date, Sport: "triathlon", Title: "triathlon",
		Legs: types.LegArray{{Sport: "swimming", Distance: 1900}, {Sport: "cycling", Distance: 90000, TransitionTime: 120}}}
	ride := models.Activity{UserID: user.ID, Date: date.Add(time.Hour), Sport: "cycling", Title: "ride"}
	for _, a := range []*models.Activity{&triathlon, &ride} {
		if err := db.Omit("User", "Equipment").Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	// the activities imported before the legs existed have NULL
	if err := db.Exec("UPDATE activities SET legs = NULL WHERE id = ?", ride.ID).Error; err != nil {
		t.Fatal(err)
	}

	got, err := db.GetActivity(triathlon.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Legs) != 2 || got.Legs[1].Sport != "cycling" || got.Legs[1].TransitionTime != 120 {
		t.Errorf("legs %+v", got.Legs)
	}
	got, err = db.GetActivity(ride.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Legs) != 0 {
		t.Errorf("a single-session activity has legs %+v", got.Legs)
	}
}

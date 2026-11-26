package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWorkplaceCommutes(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "worker", Email: "worker@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 3, 2, 7, 0, 0, 0, time.UTC)
	bike := models.Equipments{Name: "commuter", UserID: int(user.ID), IsCommute: true, DateOfPurchase: day}
	if err := db.CreateEquipment(&bike); err != nil {
		t.Fatal(err)
	}
	office := models.Workplace{UserID: user.ID, Name: "office", Lat: 45, Lon: 5, Diameter: 300}
	if err := db.SaveWorkplace(&office); err != nil {
		t.Fatal(err)
	}
	home := &types.GpsPoint{Lat: 45.05, Lon: 5}
	work := &types.GpsPoint{Lat: 45.0005, Lon: 5.0005}
	newActivity := func(sport string, date time.Time, start, end *types.GpsPoint) models.Activity {
		return models.Activity{User: user, Sport: sport, Title: sport, Date: date, Distance: 6000,
			StartPosition: start, EndPosition: end}
	}
	reload := func(a models.Activity) *models.Activity {
		t.Helper()
		got, err := db.GetActivity(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// imported: from home to the office, from the office back home, a loop from home, a run to the office
	toWork := newActivity("cycling", day.AddDate(0, 0, 1), home, work)
	fromWork := newActivity("cycling", day.AddDate(0, 0, 1).Add(10*time.Hour), work, home)
	loop := newActivity("cycling", day.AddDate(0, 0, 2), home, home)
	run := newActivity("running", day.AddDate(0, 0, 3), home, work)
	for _, a := range []*models.Activity{&toWork, &fromWork, &loop, &run} {
		if err := CreateActivity(db, a, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []models.Activity{toWork, fromWork, run} {
		if got := reload(a); !got.IsCommute || got.Co2 == 0 {
			t.Errorf("%s of %s: not a commute (%v, %v g)", a.Sport, a.Date, got.IsCommute, got.Co2)
		}
	}
	if got := reload(toWork); got.EquipmentID == nil || *got.EquipmentID != bike.ID {
		t.Error("a commute ride without the commute bike")
	}
	if got := reload(run); got.EquipmentID != nil {
		t.Error("a run got the commute bike")
	}
	if reload(loop).IsCommute {
		t.Error("a loop from home is a commute")
	}

	// recorded before the workplace: made commutes on demand, the bike only since its purchase
	if err := db.DeleteWorkplace(user.ID, office.ID); err != nil {
		t.Fatal(err)
	}
	older := newActivity("cycling", day.AddDate(0, 0, -5), work, home)
	recent := newActivity("cycling", day.AddDate(0, 0, 5), home, work)
	for _, a := range []*models.Activity{&older, &recent} {
		if err := CreateActivity(db, a, nil); err != nil {
			t.Fatal(err)
		}
	}
	if reload(recent).IsCommute {
		t.Error("a commute without a workplace")
	}
	office.ID = uuid.Nil
	if err := db.SaveWorkplace(&office); err != nil {
		t.Fatal(err)
	}
	updated, err := ApplyWorkplace(db, user.ID, office)
	if err != nil || updated != 2 {
		t.Fatalf("%d activities became commutes, want 2: %v", updated, err)
	}
	if got := reload(older); !got.IsCommute || got.Co2 == 0 || got.EquipmentID != nil {
		t.Errorf("before the purchase: commute %v, co2 %v, equipment %v", got.IsCommute, got.Co2, got.EquipmentID)
	}
	if got := reload(recent); !got.IsCommute || got.EquipmentID == nil || *got.EquipmentID != bike.ID {
		t.Errorf("after the purchase: commute %v, equipment %v", got.IsCommute, got.EquipmentID)
	}
	if updated, err := ApplyWorkplace(db, user.ID, office); err != nil || updated != 0 {
		t.Errorf("applied again: %d, %v", updated, err)
	}
}

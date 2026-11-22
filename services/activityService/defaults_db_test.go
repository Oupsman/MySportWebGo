package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
	"time"
)

// TestDefaultEquipmentOfSport: an imported activity gets the default equipment of its sport, and its weight
func TestDefaultEquipmentOfSport(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "persport", Email: "persport@test.local", Weight: 70}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	bike := models.Equipments{Name: "bike", UserID: int(user.ID), Weight: 8, DefaultSports: []string{"cycling"}}
	shoes := models.Equipments{Name: "shoes", UserID: int(user.ID), DefaultSports: []string{"running"}}
	for _, e := range []*models.Equipments{&bike, &shoes} {
		if err := db.CreateEquipment(e); err != nil {
			t.Fatal(err)
		}
	}
	for sport, want := range map[string]*models.Equipments{"cycling": &bike, "running": &shoes, "swimming": nil} {
		activity := models.Activity{UserID: user.ID, Sport: sport}
		if err := AssignDefaultEquipment(db, &activity); err != nil {
			t.Fatal(err)
		}
		switch {
		case want == nil && activity.EquipmentID != nil:
			t.Errorf("%s: equipment %v, want none", sport, *activity.EquipmentID)
		case want != nil && (activity.EquipmentID == nil || *activity.EquipmentID != want.ID ||
			activity.Equipment.ID != want.ID):
			t.Errorf("%s: equipment %v, want %s", sport, activity.EquipmentID, want.Name)
		}
	}

	weight := ImportWeight(db, user.ID)
	date := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for sport, want := range map[string]float64{
		"cycling":   78,                          // the rider and the default bike
		"running":   70,                          // the shoes weigh nothing: the equipment doesn't count
		"triathlon": 70 + DefaultEquipmentWeight, // no default: a default bike for the estimated power
	} {
		got, err := weight(date, sport)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: %v kg, want %v", sport, got, want)
		}
	}
}

func TestSportNames(t *testing.T) {
	names := SportNames()
	for _, name := range []string{"cycling", "running", "generic", "cross_country_skiing"} {
		if !IsSportName(name) {
			t.Errorf("%s is not a sport", name)
		}
	}
	for _, name := range []string{"", "Cycling", "SportInvalid(200)", "biking"} {
		if IsSportName(name) {
			t.Errorf("%q is a sport", name)
		}
	}
	for i, name := range names {
		if !IsSportName(name) || (i > 0 && names[i-1] >= name) {
			t.Fatalf("names not sorted unique sports: %v", names)
		}
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
	"time"
)

func TestEquipmentsStats(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "gear", Email: "gear@test.local"}
	other := models.Users{Username: "other", Email: "other@test.local"}
	for _, u := range []*models.Users{&user, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	bike := models.Equipments{Name: "Bike", UserID: int(user.ID), InitialMileage: 1000}
	shoes := models.Equipments{Name: "Shoes", UserID: int(user.ID), IsDefault: true}
	unused := models.Equipments{Name: "Unused", UserID: int(user.ID)}
	theirs := models.Equipments{Name: "Theirs", UserID: int(other.ID)}
	for _, e := range []*models.Equipments{&bike, &shoes, &unused, &theirs} {
		if err := db.CreateEquipment(e); err != nil {
			t.Fatal(err)
		}
	}
	date := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for i, a := range []models.Activity{
		{UserID: user.ID, Sport: "cycling", Date: date, Distance: 20400, Duration: 3600, EquipmentID: &bike.ID},
		{UserID: user.ID, Sport: "cycling", Date: date.AddDate(0, 0, 5), Distance: 30000, Duration: 4000, EquipmentID: &bike.ID},
		{UserID: user.ID, Sport: "running", Date: date, Distance: 10000, Duration: 3000, EquipmentID: &shoes.ID},
		{UserID: other.ID, Sport: "cycling", Date: date, Distance: 99000, Duration: 9000, EquipmentID: &theirs.ID},
	} {
		a.Title = "a"
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatalf("activity %d: %v", i, err)
		}
	}

	stats, err := db.GetEquipmentsStats(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Fatalf("%d equipments, want the user's 3", len(stats))
	}
	// the default one first, then by name
	if stats[0].Name != "Shoes" || stats[1].Name != "Bike" || stats[2].Name != "Unused" {
		t.Errorf("order %s %s %s", stats[0].Name, stats[1].Name, stats[2].Name)
	}
	b := stats[1]
	if b.Activities != 2 || b.Distance != 50400 || b.Duration != 7600 || b.Mileage != 1050 {
		t.Errorf("bike: %d activities %v m %d s mileage %d", b.Activities, b.Distance, b.Duration, b.Mileage)
	}
	if b.LastUsed == nil || !b.LastUsed.Equal(date.AddDate(0, 0, 5)) {
		t.Errorf("bike last used %v", b.LastUsed)
	}
	if u := stats[2]; u.Activities != 0 || u.Distance != 0 || u.LastUsed != nil || u.ID != unused.ID {
		t.Errorf("unused: %+v", u)
	}

	one, err := db.GetEquipmentStats(user.ID, bike.ID)
	if err != nil || one.Mileage != 1050 {
		t.Errorf("one equipment: mileage %d, %v", one.Mileage, err)
	}
	if _, err := db.GetEquipmentStats(user.ID, theirs.ID); err == nil {
		t.Error("the equipment of another user was returned")
	}
}

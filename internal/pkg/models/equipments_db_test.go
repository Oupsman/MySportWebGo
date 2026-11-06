package models

import (
	"testing"
	"time"
)

func TestEquipmentStatsKmSinceService(t *testing.T) {
	db := healthTestDB(t)
	if err := db.Exec("TRUNCATE services, activities, equipments CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := Users{Username: "rider", Email: "rider@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	bike := Equipments{Name: "bike", UserID: int(user.ID), InitialMileage: 100}
	if err := db.CreateEquipment(&bike); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	for i, distance := range []float64{40000, 25000, 12400} {
		activity := Activity{UserID: user.ID, Sport: "cycling", Date: day.AddDate(0, 0, i*10),
			Distance: distance, EquipmentID: &bike.ID}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
	}

	stats, err := db.GetEquipmentStats(user.ID, bike.ID)
	if err != nil {
		t.Fatal(err)
	}
	// no service yet: every activity counts
	if stats.LastService != nil || stats.KmSinceService != 77 || stats.Mileage != 177 {
		t.Fatalf("without service: last %v, since %d, mileage %d", stats.LastService, stats.KmSinceService, stats.Mileage)
	}

	// serviced between the first and the second activity, an older service doesn't matter
	for _, date := range []time.Time{day.AddDate(0, 0, 5), day.AddDate(0, 0, -30)} {
		if err := db.CreateService(&Services{EquipmentID: bike.ID, Date: date, Mileage: 140}); err != nil {
			t.Fatal(err)
		}
	}
	stats, err = db.GetEquipmentStats(user.ID, bike.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.LastService == nil || !stats.LastService.Equal(day.AddDate(0, 0, 5)) {
		t.Fatalf("last service %v", stats.LastService)
	}
	// the join on the services must not count the activities twice
	if stats.KmSinceService != 37 || stats.Mileage != 177 || stats.Activities != 3 {
		t.Fatalf("with services: since %d, mileage %d, activities %d", stats.KmSinceService, stats.Mileage, stats.Activities)
	}

	// the services go with the equipment
	if err := db.DeleteEquipment(bike.ID); err != nil {
		t.Fatal(err)
	}
	services, err := db.GetServices(bike.ID)
	if err != nil || len(services) != 0 {
		t.Fatalf("services left: %v, %v", services, err)
	}
}

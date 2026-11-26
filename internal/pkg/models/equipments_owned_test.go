package models

import (
	"testing"
	"time"
)

func TestOwnedOn(t *testing.T) {
	purchase := time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC)
	bike := Equipments{DateOfPurchase: purchase}
	for _, test := range []struct {
		name string
		date time.Time
		want bool
	}{
		{"the day before", purchase.Add(-time.Hour), false},
		{"the day of the purchase", purchase.Add(7 * time.Hour), true},
		{"after", purchase.AddDate(1, 0, 0), true},
	} {
		if got := bike.OwnedOn(test.date); got != test.want {
			t.Errorf("%s: %v, want %v", test.name, got, test.want)
		}
	}
	// bought at noon, a ride the same morning
	if !(Equipments{DateOfPurchase: purchase.Add(12 * time.Hour)}).OwnedOn(purchase.Add(8 * time.Hour)) {
		t.Error("a ride of the day of the purchase, before its hour")
	}
	if !(Equipments{}).OwnedOn(time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("unknown purchase date: not owned")
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
)

func TestRouteCommute(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "commuter", Email: "commuter@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID), IsDefault: true}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}
	create := func(name string) models.Activity {
		activity := importFit(t, name, user, equipment)
		if err := CreateActivity(db, &activity, nil); err != nil {
			t.Fatal(err)
		}
		return activity
	}
	first := create(sameRouteFiles[0])
	second := create(sameRouteFiles[1])
	other := create(otherRouteFile)
	// a commute without a saved CO2, as when the activity isn't a ride
	if err := db.Model(&second).Update("co2", 0).Error; err != nil {
		t.Fatal(err)
	}
	if first.RouteID == nil || other.RouteID == nil || *first.RouteID == *other.RouteID {
		t.Fatalf("routes: %v, %v", first.RouteID, other.RouteID)
	}

	updated, err := db.SetRouteCommute(*first.RouteID, true)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 2 {
		t.Errorf("%d activities updated, want 2", updated)
	}
	check := func(activity models.Activity, isCommute bool) models.Activity {
		t.Helper()
		got, err := db.GetActivity(activity.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.IsCommute != isCommute {
			t.Errorf("activity %s: is_commute %v, want %v", got.ID, got.IsCommute, isCommute)
		}
		return *got
	}
	check(first, true)
	if got := check(second, true); got.Co2 <= 0 || got.Co2 != Commute(got) {
		t.Errorf("missing CO2 not computed: %f", got.Co2)
	}
	check(other, false)

	// activities imported later on a commute route are commutes
	third := create(sameRouteFiles[2])
	if *third.RouteID != *first.RouteID {
		t.Fatalf("third activity on route %v", third.RouteID)
	}
	if got := check(third, true); got.Co2 <= 0 {
		t.Errorf("imported commute without CO2")
	}

	if updated, err = db.SetRouteCommute(*first.RouteID, false); err != nil || updated != 3 {
		t.Fatalf("unmark: %d, %v", updated, err)
	}
	check(first, false)
	check(third, false)
	route, err := db.GetRoute(*first.RouteID)
	if err != nil || route.IsCommute {
		t.Fatalf("route still a commute: %+v, %v", route, err)
	}
	fourth := create(sameRouteFiles[3])
	check(fourth, false)
}

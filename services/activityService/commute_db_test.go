package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"

	"github.com/google/uuid"
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

func TestCommuteEquipment(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "bikecommuter", Email: "bikecommuter@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	road := models.Equipments{Name: "road bike", UserID: int(user.ID), IsDefault: true}
	if err := db.CreateEquipment(&road); err != nil {
		t.Fatal(err)
	}
	create := func(name string) models.Activity {
		t.Helper()
		activity := importFit(t, name, user, road)
		if err := CreateActivity(db, &activity, nil); err != nil {
			t.Fatal(err)
		}
		return activity
	}
	equipmentOf := func(activity models.Activity) uuid.UUID {
		t.Helper()
		got, err := db.GetActivity(activity.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.EquipmentID == nil {
			return uuid.Nil
		}
		return *got.EquipmentID
	}
	first := create(sameRouteFiles[0])
	second := create(sameRouteFiles[1])
	other := create(otherRouteFile)
	if equipmentOf(first) != road.ID {
		t.Fatalf("imported ride without the default equipment")
	}

	// no commute bike yet: marking the route keeps the equipment
	if _, err := db.SetRouteCommute(*first.RouteID, true); err != nil {
		t.Fatal(err)
	}
	if equipmentOf(first) != road.ID {
		t.Error("equipment changed without commute bike")
	}
	if _, err := db.SetRouteCommute(*first.RouteID, false); err != nil {
		t.Fatal(err)
	}

	// only one commute bike per user
	old := models.Equipments{Name: "old commuter", UserID: int(user.ID), IsCommute: true}
	city := models.Equipments{Name: "city bike", UserID: int(user.ID), IsCommute: true}
	for _, e := range []*models.Equipments{&old, &city} {
		if err := db.CreateEquipment(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&old, "id = ?", old.ID).Error; err != nil || old.IsCommute {
		t.Fatalf("the previous commute bike is still flagged: %v", err)
	}
	if err := db.First(&road, "id = ?", road.ID).Error; err != nil || !road.IsDefault {
		t.Fatalf("the commute flag changed the default equipment: %v", err)
	}

	// marking a route as commute gives its rides the commute bike
	if _, err := db.SetRouteCommute(*first.RouteID, true); err != nil {
		t.Fatal(err)
	}
	if equipmentOf(first) != city.ID || equipmentOf(second) != city.ID {
		t.Error("the rides of a commute route don't have the commute bike")
	}
	if equipmentOf(other) != road.ID {
		t.Error("a ride of another route changed")
	}
	// a ride imported on a commute route gets it instead of the default one
	third := create(sameRouteFiles[2])
	if equipmentOf(third) != city.ID {
		t.Error("imported commute without the commute bike")
	}

	// editing a ride into a commute
	got, err := db.GetActivity(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		activity  models.Activity
		isCommute bool
		requested *uuid.UUID
		want      uuid.UUID
	}{
		{"becomes a commute", *got, true, &road.ID, city.ID},
		{"becomes a commute with another equipment chosen", *got, true, &old.ID, old.ID},
		{"stays a normal ride", *got, false, &road.ID, road.ID},
	} {
		equipment, err := EquipmentOnUpdate(db, test.activity, test.isCommute, test.requested)
		if err != nil {
			t.Fatal(err)
		}
		if equipment == nil || *equipment != test.want {
			t.Errorf("%s: equipment %v, want %v", test.name, equipment, test.want)
		}
	}
	// already a commute: the user may ride another bike, it is kept
	commute, err := db.GetActivity(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if equipment, err := EquipmentOnUpdate(db, *commute, true, &road.ID); err != nil || *equipment != road.ID {
		t.Errorf("an edited commute lost its equipment: %v, %v", equipment, err)
	}

	// the commute bike given to the commute rides recorded before: not to the runs nor the other rides
	if err := db.Model(&models.Activity{}).Where("id = ?", second.ID).Update("equipment_id", road.ID).Error; err != nil {
		t.Fatal(err)
	}
	run := models.Activity{UserID: user.ID, Sport: "running", Title: "run", Date: first.Date.AddDate(0, 0, -3),
		IsCommute: true, EquipmentID: &road.ID}
	if err := db.Omit("User", "Equipment").Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := db.AssignCommuteEquipment(user.ID, city.ID)
	if err != nil || updated != 1 {
		t.Fatalf("assigned to %d rides, want 1: %v", updated, err)
	}
	if equipmentOf(second) != city.ID || equipmentOf(run) != road.ID || equipmentOf(other) != road.ID {
		t.Error("the commute bike went to the wrong activities")
	}
}

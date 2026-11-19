package models

import (
	"MySportWeb/internal/pkg/types"
	"reflect"
	"testing"
	"time"
)

func TestGetWeeklyHrZones(t *testing.T) {
	db := healthTestDB(t)
	if err := db.Exec("TRUNCATE activities CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := Users{Username: "zones", Email: "zones@test.local"}
	other := Users{Username: "other", Email: "other@test.local"}
	for _, u := range []*Users{&user, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	add := func(userID uint, date time.Time, zones types.FloatArray) {
		activity := Activity{UserID: userID, Date: date, HrZones: zones}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Wednesday 18 November 2026 in Paris: the current week starts on Monday 16
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, paris)
	add(user.ID, time.Date(2026, 11, 17, 7, 0, 0, 0, paris), types.FloatArray{100, 200, 0, 0, 0})
	add(user.ID, time.Date(2026, 11, 18, 7, 0, 0, 0, paris), types.FloatArray{0, 50, 10, 0, 1})
	// Monday 00:30 in Paris is still Sunday in UTC: it's in the current week
	add(user.ID, time.Date(2026, 11, 16, 0, 30, 0, 0, paris), types.FloatArray{0, 0, 0, 7, 0})
	// Sunday 23:30 in Paris: the week before
	add(user.ID, time.Date(2026, 11, 15, 23, 30, 0, 0, paris), types.FloatArray{3, 0, 0, 0, 0})
	// before the 2 weeks asked, not computed yet, another user's
	add(user.ID, time.Date(2026, 11, 1, 10, 0, 0, 0, paris), types.FloatArray{999, 0, 0, 0, 0})
	add(user.ID, time.Date(2026, 11, 17, 10, 0, 0, 0, paris), nil)
	add(other.ID, time.Date(2026, 11, 17, 10, 0, 0, 0, paris), types.FloatArray{999, 0, 0, 0, 0})

	weeks, err := db.GetWeeklyHrZones(user.ID, now, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []types.WeeklyHrZones{
		{Week: "2026-11-09", Zones: []float64{3, 0, 0, 0, 0}},
		{Week: "2026-11-16", Zones: []float64{100, 250, 10, 7, 1}},
	}
	if !reflect.DeepEqual(weeks, want) {
		t.Errorf("weeks %v, want %v", weeks, want)
	}

	// across the end of the daylight saving time (25 October): the weeks still start on Mondays
	weeks, err = db.GetWeeklyHrZones(user.ID, now, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i, week := range []string{"2026-10-19", "2026-10-26", "2026-11-02", "2026-11-09", "2026-11-16"} {
		if weeks[i].Week != week {
			t.Errorf("week %d starts on %s, want %s", i, weeks[i].Week, week)
		}
	}
}

func TestHrZonesNullAndReset(t *testing.T) {
	db := healthTestDB(t)
	if err := db.Exec("TRUNCATE activities CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := Users{Username: "reset", Email: "reset@test.local"}
	other := Users{Username: "kept", Email: "kept@test.local"}
	for _, u := range []*Users{&user, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	computed := types.FloatArray{1, 2, 3, 4, 5}
	mine := Activity{UserID: user.ID, Date: time.Now(), HrZones: computed}
	theirs := Activity{UserID: other.ID, Date: time.Now(), HrZones: computed}
	// a nil array is saved as JSON null: it's not computed either
	unsaved := Activity{UserID: other.ID, Date: time.Now()}
	for _, a := range []*Activity{&mine, &theirs, &unsaved} {
		if err := db.Omit("User", "Equipment").Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	if ids, err := db.GetActivitiesWithoutHrZones(); err != nil || len(ids) != 1 || ids[0] != unsaved.ID {
		t.Fatalf("activities without zones %v (%v), want only %s", ids, err, unsaved.ID)
	}
	if err := db.ResetHrZones(user.ID); err != nil {
		t.Fatal(err)
	}
	if ids, err := db.GetActivitiesWithoutHrZones(); err != nil || len(ids) != 2 {
		t.Fatalf("%d activities without zones after the reset (%v), want 2", len(ids), err)
	}
	// a SQL NULL is read back
	var reset Activity
	if err := db.First(&reset, "id = ?", mine.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reset.HrZones != nil || reset.HrZoneBounds != nil {
		t.Errorf("zones %v bounds %v after the reset, want none", reset.HrZones, reset.HrZoneBounds)
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRefuseDuplicates(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "dup", Email: "dup@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID), IsDefault: true}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}
	fresh := func() models.Activity {
		a := importFit(t, sameRouteFiles[0], user, equipment)
		a.ID = uuid.New()
		return a
	}

	first := fresh()
	if err := CreateActivity(db, &first, nil); err != nil {
		t.Fatal(err)
	}
	again := fresh()
	err := CreateActivity(db, &again, nil)
	var duplicate *DuplicateError
	if !errors.As(err, &duplicate) || duplicate.ExistingID != first.ID {
		t.Fatalf("second import of the same file: %v, want a duplicate of %s", err, first.ID)
	}

	// same start, another sport: a different activity
	other := fresh()
	other.Sport = "running"
	if err := CreateActivity(db, &other, nil); err != nil {
		t.Fatalf("same start time, other sport: %v", err)
	}

	// the same file uploaded several times at once: one import, its file stored once
	newFile := importFit(t, sameRouteFiles[1], user, equipment)
	var created, refused, stored atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		a := newFile
		a.ID = uuid.New()
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := CreateActivity(db, &a, func(*models.Activity) error { stored.Add(1); return nil })
			switch {
			case err == nil:
				created.Add(1)
			case errors.As(err, new(*DuplicateError)):
				refused.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 || refused.Load() != 5 || stored.Load() != 1 {
		t.Fatalf("concurrent uploads: %d created, %d refused, %d files stored", created.Load(), refused.Load(), stored.Load())
	}
}

func TestFindDuplicates(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "dups", Email: "dups@test.local"}
	other := models.Users{Username: "other", Email: "other@test.local"}
	for _, u := range []*models.Users{&user, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	add := func(owner models.Users, title, sport string, offset time.Duration, minutes uint32) {
		a := models.Activity{UserID: owner.ID, Title: title, Sport: sport, Date: start.Add(offset),
			Duration: minutes * 60, Distance: float64(minutes) * 300}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	// the same file imported three times
	add(user, "copy 1", "cycling", 0, 40)
	add(user, "copy 2", "cycling", 0, 40)
	add(user, "copy 3", "cycling", 0, 40)
	// the same ride on a watch and a bike computer, started a minute apart
	add(user, "watch", "cycling", 3*time.Hour, 60)
	add(user, "bike computer", "cycling", 3*time.Hour+time.Minute, 58)
	// a short run during a ride: another sport
	add(user, "run", "running", 3*time.Hour+10*time.Minute, 5)
	// two rides one after the other, overlapping for less than half of the shorter
	add(user, "ride A", "cycling", 6*time.Hour, 30)
	add(user, "ride B", "cycling", 6*time.Hour+20*time.Minute, 30)
	// someone else's ride at the same time
	add(other, "someone else", "cycling", 0, 40)

	groups, err := db.FindDuplicates(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, g := range groups {
		titles := []string{}
		for _, a := range g.Activities {
			titles = append(titles, a.Title)
		}
		got = append(got, fmt.Sprintf("%v %v", g.Identical, titles))
	}
	// most recent first, activities in import order
	want := []string{"false [watch bike computer]", "true [copy 1 copy 2 copy 3]"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("groups %v, want %v", got, want)
	}
}

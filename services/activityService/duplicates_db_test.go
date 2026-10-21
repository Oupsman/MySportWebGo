package activityService

import (
	"MySportWeb/internal/pkg/models"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

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

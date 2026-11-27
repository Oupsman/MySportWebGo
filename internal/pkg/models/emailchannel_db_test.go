package models

import (
	"sync"
	"testing"
)

// Turned on by requests at the same time (a switch clicked twice, two tabs), a user still has one email
// channel: two would send each notification twice
func TestSetEmailChannelConcurrent(t *testing.T) {
	db := healthTestDB(t)
	user := Users{Username: "alice", Email: "alice@test.local", Role: RoleUser}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 5; round++ {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- db.SetEmailChannel(user.ID, true)
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var n int64
		db.Model(&Channel{}).Where("user_id = ? AND channel_provider = ?", user.ID, EmailProvider).Count(&n)
		if n != 1 {
			t.Fatalf("round %d: %d email channels", round, n)
		}
		if queued, err := db.NotifyUser(user.ID, NotifyTest, "{}"); err != nil || queued != 1 {
			t.Fatalf("round %d: queued for %d channels (%v)", round, queued, err)
		}
		if err := db.SetEmailChannel(user.ID, false); err != nil {
			t.Fatal(err)
		}
	}
}

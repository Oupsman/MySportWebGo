package notifications

import (
	"MySportWeb/internal/pkg/models"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestCheckGoals(t *testing.T) {
	db := testDB(t)
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User", Timezone: "Europe/Paris"}
	bob := models.Users{Username: "bob", Email: "bob@test.local", Role: "User"}
	for _, u := range []*models.Users{&alice, &bob} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	// alice gets the notifications by email, bob has no channel
	if err := db.Omit("User").Create(&models.Channel{UserID: uint64(alice.ID), ChannelProvider: models.EmailProvider, Name: "email"}).Error; err != nil {
		t.Fatal(err)
	}
	paris, _ := time.LoadLocation("Europe/Paris")
	// Wednesday April 15 2026
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, paris)
	monthly := models.Goal{UserID: alice.ID, Metric: "distance", Target: 100000, Sports: models.SportList{"cycling"}, Period: "month"}
	bobs := models.Goal{UserID: bob.ID, Metric: "distance", Target: 1000, Period: "month"}
	for _, g := range []*models.Goal{&monthly, &bobs} {
		if err := db.SaveGoal(g, now); err != nil {
			t.Fatal(err)
		}
	}
	ride := func(u models.Users, date time.Time, meters float64) {
		t.Helper()
		a := models.Activity{ID: uuid.New(), UserID: u.ID, Title: "ride", Sport: "cycling", Date: date, Distance: meters}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	messages := func() []PushMessage {
		t.Helper()
		var queued []models.Notifications
		db.Order("id").Find(&queued)
		result := make([]PushMessage, len(queued))
		for i, n := range queued {
			json.Unmarshal([]byte(n.Message), &result[i])
		}
		return result
	}

	ride(alice, now.Add(-24*time.Hour), 60000)
	ride(bob, now.Add(-24*time.Hour), 60000)
	if err := CheckGoals(*db, zerolog.Nop(), now); err != nil {
		t.Fatal(err)
	}
	if m := messages(); len(m) != 0 {
		t.Fatalf("not reached yet: %+v", m)
	}
	ride(alice, now.Add(-time.Hour), 45000)
	if err := CheckGoals(*db, zerolog.Nop(), now); err != nil {
		t.Fatal(err)
	}
	m := messages()
	if len(m) != 1 || m[0].Title != "Goal reached" || !strings.Contains(m[0].Body, "Cycling this month: 105.0 km, for a target of 100.0 km") {
		t.Fatalf("reached: %+v", m)
	}
	// once per period
	if err := CheckGoals(*db, zerolog.Nop(), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if m := messages(); len(m) != 1 {
		t.Errorf("twice: %+v", m)
	}
	// the next month is another period
	may := time.Date(2026, 5, 20, 12, 0, 0, 0, paris)
	ride(alice, may.Add(-time.Hour), 120000)
	if err := CheckGoals(*db, zerolog.Nop(), may); err != nil {
		t.Fatal(err)
	}
	if m := messages(); len(m) != 2 {
		t.Errorf("next month: %+v", m)
	}
}

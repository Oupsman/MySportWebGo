package notifications

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// an upload setting a best still standing is notified; an old one (an import), or one without, isn't
func TestNotifyHighlights(t *testing.T) {
	db := testDB(t)
	carol := models.Users{Username: "carol", Email: "carol@test.local", Role: "User"}
	if err := db.Create(&carol).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("User").Create(&models.Channel{UserID: uint64(carol.ID), ChannelProvider: models.EmailProvider, Name: "email"}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	run := func(date time.Time, seconds float64) models.Activity {
		t.Helper()
		a := models.Activity{ID: uuid.New(), UserID: carol.ID, Title: "Evening run", Sport: "running", Date: date,
			BestEfforts: types.BestEffortArray{{Sport: "running", Kind: types.EffortDistance, Target: 5000, Value: seconds}}}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a
	}
	notify := func(a models.Activity) []PushMessage {
		t.Helper()
		db.Where("1 = 1").Delete(&models.Notifications{})
		if err := NotifyHighlights(db, zerolog.Nop(), carol.ID, a.ID, a.Title, a.Date, time.UTC, now); err != nil {
			t.Fatal(err)
		}
		var queued []models.Notifications
		db.Find(&queued)
		messages := make([]PushMessage, len(queued))
		for i, n := range queued {
			json.Unmarshal([]byte(n.Message), &messages[i])
		}
		return messages
	}

	first := run(now.AddDate(0, 0, -30), 1500)
	if m := notify(first); len(m) != 0 {
		t.Errorf("the first run: %v", m)
	}
	// a best, a month ago: an import, not notified
	old := run(now.AddDate(0, 0, -20), 1450)
	if m := notify(old); len(m) != 0 {
		t.Errorf("an old best: %v", m)
	}
	best := run(now.Add(-2*time.Hour), 1400)
	m := notify(best)
	if len(m) != 1 || m[0].Title != "New best: Evening run" || m[0].Body != "5 km in 23:20 (−0:50)." ||
		m[0].URL != "/#/activity/"+best.ID.String() {
		t.Errorf("the new best: %+v", m)
	}
	slower := run(now.Add(-time.Hour), 1600)
	if m := notify(slower); len(m) != 0 {
		t.Errorf("a slower run: %v", m)
	}
}

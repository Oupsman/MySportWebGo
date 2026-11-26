package notifications

import (
	"MySportWeb/internal/pkg/models"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestCheersMessage(t *testing.T) {
	id := uuid.New()
	for names, want := range map[string]string{
		"alice":                 "alice cheered your activity",
		"alice,bob":             "alice and bob cheered your activity",
		"alice,bob,carol":       "alice, bob and carol cheered your activity",
		"alice,bob,carol,dave":  "alice, bob and 2 others cheered your activity",
		"alice,bob,c,d,e,f,g,h": "alice, bob and 6 others cheered your activity",
	} {
		m := cheersMessage(models.CheerBatch{ActivityID: id, Title: "Ride", Usernames: strings.Split(names, ",")})
		if m.Title != want || m.Body != "Ride" || m.URL != "/#/activity/"+id.String() {
			t.Errorf("%s: %+v", names, m)
		}
	}
	m := CommentMessage("bob", id, "Ride", "  so\n\nfast  "+strings.Repeat("x", 200))
	if m.Title != "bob commented on Ride" || !strings.HasPrefix(m.Body, "so fast xx") ||
		len([]rune(m.Body)) != commentShown || !strings.HasSuffix(m.Body, "…") {
		t.Errorf("comment: %+v", m)
	}
	if m := CommentMessage("bob", id, "Ride", "short"); m.Body != "short" {
		t.Errorf("short comment: %+v", m)
	}
}

func TestCheckCheers(t *testing.T) {
	db := testDB(t)
	var users []models.Users
	for _, name := range []string{"owner", "quiet", "alice", "bob", "carol"} {
		u := models.Users{Username: name, Email: name + "@test.local", Role: models.RoleUser}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.SetEmailChannel(u.ID, true); err != nil {
			t.Fatal(err)
		}
		users = append(users, u)
	}
	owner, quiet, alice, bob, carol := users[0], users[1], users[2], users[3], users[4]
	if err := db.SetNotifySocial(quiet.ID, false); err != nil {
		t.Fatal(err)
	}
	activity := func(u models.Users, title string) uuid.UUID {
		a := models.Activity{ID: uuid.New(), UserID: u.ID, Title: title, Sport: "cycling", Date: time.Now()}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	ride, run, quietRide := activity(owner, "Ride"), activity(owner, "Run"), activity(quiet, "Quiet ride")
	now := time.Now()
	cheer := func(activityID uuid.UUID, u models.Users, ago time.Duration) {
		c := models.Cheer{ActivityID: activityID, UserID: u.ID, CreatedAt: now.Add(-ago)}
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	cheer(ride, bob, 20*time.Minute)
	cheer(ride, alice, 10*time.Minute)
	cheer(ride, carol, time.Minute) // not settled yet
	cheer(run, carol, 30*time.Minute)
	cheer(quietRide, alice, 30*time.Minute)

	if err := CheckCheers(*db, zerolog.Nop(), now); err != nil {
		t.Fatal(err)
	}
	queued := messagesOf(t, db, owner.ID)
	if len(queued) != 2 || !strings.Contains(queued[0], "carol cheered your activity") ||
		!strings.Contains(queued[1], "bob and alice cheered your activity") {
		t.Errorf("owner: %q", queued)
	}
	if q := messagesOf(t, db, quiet.ID); len(q) != 0 {
		t.Errorf("notified while off: %q", q)
	}

	// once each: the settled ones are marked, the quiet owner's too; carol's comes at the next check
	if err := CheckCheers(*db, zerolog.Nop(), now); err != nil {
		t.Fatal(err)
	}
	if q := messagesOf(t, db, owner.ID); len(q) != 2 {
		t.Errorf("notified twice: %q", q)
	}
	if err := CheckCheers(*db, zerolog.Nop(), now.Add(CheerSettle)); err != nil {
		t.Fatal(err)
	}
	queued = messagesOf(t, db, owner.ID)
	if len(queued) != 3 || !strings.Contains(queued[2], `"title":"carol cheered your activity","body":"Ride"`) {
		t.Errorf("owner after: %q", queued)
	}
	var unnotified int64
	db.Model(&models.Cheer{}).Where("notified_at IS NULL").Count(&unnotified)
	if unnotified != 0 {
		t.Errorf("%d cheers unnotified", unnotified)
	}
}

// messagesOf returns the messages queued for a user, in order
func messagesOf(t *testing.T, db *models.DB, userID uint) []string {
	t.Helper()
	var messages []string
	err := db.Table("notifications n").Joins("JOIN channels c ON c.id = n.channel_id").
		Where("c.user_id = ?", userID).Order("n.id").Pluck("n.message", &messages).Error
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

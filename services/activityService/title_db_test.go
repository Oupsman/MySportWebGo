package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestBackfillTitles(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "titles", Email: "titles@test.local", Timezone: "Europe/Paris"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	bastille := &types.GpsPoint{Lat: 48.8532, Lon: 2.3692}
	add := func(title, sport string, date time.Time, start *types.GpsPoint) models.Activity {
		a := models.Activity{UserID: user.ID, Title: title, Sport: sport, Date: date, StartPosition: start}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a
	}
	// legacy titles used UTC hours: 11:30 UTC was "Morning", it's 13:30 in Paris
	lunch := add("Morning cycling", "cycling", time.Date(2026, 7, 1, 11, 30, 0, 0, time.UTC), bastille)
	swim := add("Night swimming", "swimming", time.Date(2026, 1, 10, 7, 0, 0, 0, time.UTC), nil)
	renamed := add("Sunday with friends", "cycling", time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC), bastille)
	otherSport := add("Morning cycling", "running", time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC), bastille)
	// its user has no timezone: renaming it with UTC hours couldn't be undone
	noTimezone := models.Users{Username: "notz", Email: "notz@test.local"}
	if err := db.Create(&noTimezone).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&noTimezone).Update("timezone", "").Error; err != nil {
		t.Fatal(err)
	}
	waiting := models.Activity{UserID: noTimezone.ID, Title: "Morning cycling", Sport: "cycling",
		Date: time.Date(2026, 7, 1, 11, 30, 0, 0, time.UTC), StartPosition: bastille}
	if err := db.Omit("User", "Equipment").Create(&waiting).Error; err != nil {
		t.Fatal(err)
	}

	if err := BackfillTitles(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		activity models.Activity
		want     string
	}{
		{lunch, "Paris lunch ride"},
		{swim, "Morning swim"},
		{renamed, "Sunday with friends"},
		// not a title given to a run
		{otherSport, "Morning cycling"},
		{waiting, "Morning cycling"},
	} {
		got, err := db.GetActivity(c.activity.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != c.want {
			t.Errorf("%q renamed %q, want %q", c.activity.Title, got.Title, c.want)
		}
	}

	// new titles aren't legacy ones: only the one waiting for a timezone is left
	left, err := db.GetActivitiesToRetitle(legacyTitles)
	if err != nil || len(left) != 1 || left[0].ID != waiting.ID {
		t.Errorf("activities left to rename: %+v, %v", left, err)
	}

	// once the user sets it, the activity is renamed
	if err := db.Model(&noTimezone).Update("timezone", "Europe/Paris").Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillTitles(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetActivity(waiting.ID); err != nil || got.Title != "Paris lunch ride" {
		t.Errorf("after setting the timezone: %q, %v", got.Title, err)
	}
}

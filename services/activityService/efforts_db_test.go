package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestRecords(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "records", Email: "records@test.local"}
	other := models.Users{Username: "other", Email: "other@test.local"}
	for _, u := range []*models.Users{&user, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	km := func(seconds float64) types.BestEffort {
		return types.BestEffort{Sport: "running", Kind: types.EffortDistance, Target: 1000, Value: seconds}
	}
	watts := func(value float64) types.BestEffort {
		return types.BestEffort{Sport: "cycling", Kind: types.EffortPower, Target: 60, Value: value}
	}
	add := func(owner uint, title string, date time.Time, efforts types.BestEffortArray) {
		a := models.Activity{UserID: owner, Title: title, Date: date, BestEfforts: efforts}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	day := func(d int) time.Time { return time.Date(2026, 1, d, 10, 0, 0, 0, time.UTC) }
	add(user.ID, "slow", day(1), types.BestEffortArray{km(260), watts(300)})
	add(user.ID, "fast", day(2), types.BestEffortArray{km(230), watts(250)})
	add(user.ID, "tie", day(3), types.BestEffortArray{km(230)})
	add(user.ID, "none", day(4), types.BestEffortArray{})
	add(user.ID, "not computed", day(5), nil)
	add(other.ID, "other", day(6), types.BestEffortArray{km(150), watts(500)})

	summary := func(records []types.EffortRecord) string {
		s := ""
		for _, r := range records {
			s += fmt.Sprintf("%s/%v:%d=%s %v; ", r.Kind, r.Target, r.Rank, r.ActivityTitle, r.Value)
		}
		return s
	}
	records, err := db.GetRecords(user.ID, "", nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	// by sport (cycling, running), the highest powers first, the shortest times first, the oldest first on
	// a tie, not the efforts of the others
	want := "power/60:1=slow 300; power/60:2=fast 250; " +
		"distance/1000:1=fast 230; distance/1000:2=tie 230; distance/1000:3=slow 260; "
	if got := summary(records); got != want {
		t.Errorf("records:\n%s\nwant\n%s", got, want)
	}
	if records, err = db.GetRecords(user.ID, "running", nil, 1); err != nil || summary(records) != "distance/1000:1=fast 230; " {
		t.Errorf("running record: %s %v", summary(records), err)
	}
	since := day(3)
	if records, err = db.GetRecords(user.ID, "", &since, 1); err != nil || summary(records) != "distance/1000:1=tie 230; " {
		t.Errorf("records since the 3rd: %s %v", summary(records), err)
	}
	if records[0].Date.IsZero() || records[0].ActivityID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Errorf("record without its activity: %+v", records[0])
	}

	ids, err := db.GetActivitiesWithoutBestEfforts()
	if err != nil || len(ids) != 1 {
		t.Fatalf("activities without best efforts: %v %v, want the one not computed", ids, err)
	}
	// without FIT file, from the stored series: none here, but computed
	if err := BackfillBestEfforts(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if ids, err = db.GetActivitiesWithoutBestEfforts(); err != nil || len(ids) != 0 {
		t.Errorf("activities without best efforts after the backfill: %v %v", ids, err)
	}
}

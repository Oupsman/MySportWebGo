package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
	"time"
)

func TestDashboard(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "dash", Email: "dash@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	// Wednesday 21 October 2026, the week starts on Monday 19
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, paris)
	add := func(date time.Time, sport string, km float64, minutes uint32, climb float64) {
		a := models.Activity{UserID: user.ID, Date: date, Sport: sport, Title: sport, Distance: km * 1000,
			Duration: minutes * 60, PositiveElevation: climb}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	add(time.Date(2026, 10, 19, 8, 0, 0, 0, paris), "cycling", 10, 30, 100) // this week
	add(time.Date(2026, 10, 19, 0, 30, 0, 0, paris), "running", 5, 25, 20)  // this week in Paris, Sunday in UTC
	add(time.Date(2026, 10, 18, 20, 0, 0, 0, paris), "cycling", 20, 60, 50) // last week, this month
	add(time.Date(2026, 9, 30, 18, 0, 0, 0, paris), "cycling", 30, 90, 300) // last month
	add(time.Date(2025, 12, 31, 10, 0, 0, 0, paris), "cycling", 40, 80, 10) // last year

	d, err := db.GetDashboard(user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string]struct {
		got        float64
		activities int64
		want       float64
		count      int64
	}{
		"this week":  {d.Totals.ThisWeek.Distance, d.Totals.ThisWeek.Activities, 15000, 2},
		"last week":  {d.Totals.LastWeek.Distance, d.Totals.LastWeek.Activities, 20000, 1},
		"this month": {d.Totals.ThisMonth.Distance, d.Totals.ThisMonth.Activities, 35000, 3},
		"last month": {d.Totals.LastMonth.Distance, d.Totals.LastMonth.Activities, 30000, 1},
		"this year":  {d.Totals.ThisYear.Distance, d.Totals.ThisYear.Activities, 65000, 4},
		"all time":   {d.Totals.AllTime.Distance, d.Totals.AllTime.Activities, 105000, 5},
	}
	for name, c := range totals {
		if c.got != c.want || c.activities != c.count {
			t.Errorf("%s: %d activities, %.0f m, want %d, %.0f m", name, c.activities, c.got, c.count, c.want)
		}
	}
	if d.Totals.ThisWeek.Duration != 55*60 || d.Totals.ThisWeek.Elevation != 120 {
		t.Errorf("this week: %d s, %.0f m climbed", d.Totals.ThisWeek.Duration, d.Totals.ThisWeek.Elevation)
	}

	if len(d.WeekStarts) != models.DashboardWeeks || d.WeekStarts[0] != "2026-08-03" || d.WeekStarts[11] != "2026-10-19" {
		t.Errorf("week starts: %v", d.WeekStarts)
	}
	weekly := map[string]float64{}
	for _, w := range d.Weekly {
		weekly[w.Week+" "+w.Sport] = w.Distance
	}
	want := map[string]float64{"2026-10-19 cycling": 10000, "2026-10-19 running": 5000, "2026-10-12 cycling": 20000, "2026-09-28 cycling": 30000}
	if len(weekly) != len(want) {
		t.Errorf("weekly: %v", weekly)
	}
	for k, v := range want {
		if weekly[k] != v {
			t.Errorf("weekly %s: %.0f, want %.0f (all: %v)", k, weekly[k], v, weekly)
		}
	}

	if len(d.Sports) != 2 || d.Sports[0].Sport != "cycling" || d.Sports[0].Totals.Activities != 4 {
		t.Fatalf("sports: %+v", d.Sports)
	}
	cycling := d.Sports[0]
	if cycling.LongestDistance == nil || cycling.LongestDistance.Value != 40000 ||
		cycling.LongestDuration == nil || cycling.LongestDuration.Value != 90*60 ||
		cycling.BiggestClimb == nil || cycling.BiggestClimb.Value != 300 {
		t.Errorf("cycling records: %+v %+v %+v", cycling.LongestDistance, cycling.LongestDuration, cycling.BiggestClimb)
	}

	if len(d.Recent) != 5 || d.Recent[0].Distance != 10000 || d.Recent[0].Duration != 30*60 || d.Recent[0].Elevation != 100 {
		t.Errorf("recent: %+v", d.Recent)
	}
	if len(d.Calendar) != 3 || d.Calendar[0].Title != "cycling" || d.Calendar[1].Title != "running" {
		t.Errorf("calendar: %+v", d.Calendar)
	}
	if d.Timezone != "Europe/Paris" {
		t.Errorf("timezone %q", d.Timezone)
	}
}

func TestCalendarMonths(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "calendar", Email: "calendar@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	add := func(title string, date time.Time) {
		a := models.Activity{UserID: user.ID, Date: date, Sport: "cycling", Title: title, Distance: 1000, Duration: 600}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	// both on 31 October in UTC
	add("october", time.Date(2026, 10, 31, 23, 30, 0, 0, paris))
	add("november", time.Date(2026, 11, 1, 0, 30, 0, 0, paris))
	add("september", time.Date(2026, 9, 1, 0, 0, 0, 0, paris))

	for month, want := range map[time.Time][]string{
		time.Date(2026, 9, 1, 0, 0, 0, 0, paris):  {"september"},
		time.Date(2026, 10, 1, 0, 0, 0, 0, paris): {"october"},
		time.Date(2026, 11, 1, 0, 0, 0, 0, paris): {"november"},
		time.Date(2026, 12, 1, 0, 0, 0, 0, paris): {},
	} {
		got, err := db.GetCalendar(user.ID, month)
		if err != nil {
			t.Fatal(err)
		}
		titles := []string{}
		for _, a := range got {
			titles = append(titles, a.Title)
		}
		if len(titles) != len(want) || (len(want) > 0 && titles[0] != want[0]) {
			t.Errorf("%s: %v, want %v", month.Format("2006-01"), titles, want)
		}
	}
}

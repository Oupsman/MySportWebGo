package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSearchActivities(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "search", Email: "search@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	bike := models.Equipments{Name: "Gravel", UserID: int(user.ID)}
	if err := db.Create(&bike).Error; err != nil {
		t.Fatal(err)
	}
	paris, _ := time.LoadLocation("Europe/Paris")
	add := func(title, sport string, date time.Time, km float64, equipment *uuid.UUID, commute bool) {
		a := models.Activity{UserID: user.ID, Title: title, Sport: sport, Date: date, Distance: km * 1000,
			Duration: uint32(km * 180), EquipmentID: equipment, IsCommute: commute}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	add("Commute to work", "cycling", time.Date(2026, 3, 2, 8, 0, 0, 0, paris), 12, &bike.ID, true)
	add("100% climbing", "cycling", time.Date(2026, 1, 1, 0, 30, 0, 0, paris), 40, &bike.ID, false) // 2025 in UTC
	add("100x intervals", "running", time.Date(2025, 12, 31, 18, 0, 0, 0, paris), 8, nil, false)
	add("Long run", "running", time.Date(2025, 6, 15, 9, 0, 0, 0, paris), 21, nil, false)
	add("Pool", "swimming", time.Date(2024, 2, 10, 12, 0, 0, 0, paris), 2, nil, false)

	search := func(f models.ActivityFilter, sort string, desc bool, page, perPage int) []string {
		t.Helper()
		s, err := db.SearchActivities(user.ID, f, sort, desc, page, perPage, paris)
		if err != nil {
			t.Fatal(err)
		}
		titles := []string{}
		for _, a := range s.Activities {
			titles = append(titles, a.Title)
		}
		return titles
	}
	expect := func(name string, got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s: %v, want %v", name, got, want)
			return
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: %v, want %v", name, got, want)
				return
			}
		}
	}

	all, err := db.SearchActivities(user.ID, models.ActivityFilter{}, "date", true, 1, 25, paris)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 5 || all.Totals.Distance != 83000 {
		t.Errorf("all: %d activities, %.0f m", all.Total, all.Totals.Distance)
	}
	expect("sports", all.Sports, "cycling", "running", "swimming")
	if len(all.Years) != 3 || all.Years[0] != 2026 || all.Years[2] != 2024 {
		t.Errorf("years %v, want [2026 2025 2024]", all.Years)
	}
	if all.Activities[0].EquipmentName == nil || *all.Activities[0].EquipmentName != "Gravel" || all.Activities[3].EquipmentName != nil {
		t.Errorf("equipment names: %v %v", all.Activities[0].EquipmentName, all.Activities[3].EquipmentName)
	}
	expect("default: most recent first", search(models.ActivityFilter{}, "date", true, 1, 25),
		"Commute to work", "100% climbing", "100x intervals", "Long run", "Pool")
	expect("sports", search(models.ActivityFilter{Sports: []string{"running", "swimming"}}, "date", true, 1, 25),
		"100x intervals", "Long run", "Pool")
	// the year 2026 in Paris: 1 January 00:30 is in, though still 2025 in UTC
	year := models.ActivityFilter{From: time.Date(2026, 1, 1, 0, 0, 0, 0, paris), To: time.Date(2027, 1, 1, 0, 0, 0, 0, paris)}
	expect("2026", search(year, "date", true, 1, 25), "Commute to work", "100% climbing")
	expect("literal 100%", search(models.ActivityFilter{Query: "100%"}, "date", true, 1, 25), "100% climbing")
	expect("case insensitive", search(models.ActivityFilter{Query: "LONG"}, "date", true, 1, 25), "Long run")
	expect("equipment", search(models.ActivityFilter{EquipmentID: &bike.ID}, "date", true, 1, 25), "Commute to work", "100% climbing")
	expect("no equipment", search(models.ActivityFilter{NoEquipment: true}, "date", true, 1, 25), "100x intervals", "Long run", "Pool")
	expect("commute", search(models.ActivityFilter{CommuteOnly: true}, "date", true, 1, 25), "Commute to work")
	expect("distance ascending", search(models.ActivityFilter{}, "distance", false, 1, 25),
		"Pool", "100x intervals", "Commute to work", "Long run", "100% climbing")

	// pages of 2: every activity once
	seen := map[string]int{}
	for page := 1; page <= 3; page++ {
		for _, title := range search(models.ActivityFilter{}, "title", false, page, 2) {
			seen[title]++
		}
	}
	if len(seen) != 5 {
		t.Errorf("pages: %v", seen)
	}
	for title, n := range seen {
		if n != 1 {
			t.Errorf("%s on %d pages", title, n)
		}
	}
}

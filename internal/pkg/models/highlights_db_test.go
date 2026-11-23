package models

import (
	"MySportWeb/internal/pkg/types"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHighlights(t *testing.T) {
	db := healthTestDB(t)
	user := Users{Username: "highlights", Email: "highlights@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	// a Monday
	monday := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	five := func(seconds float64) types.BestEffort {
		return types.BestEffort{Sport: "running", Kind: types.EffortDistance, Target: 5000, Value: seconds, StartOffset: 60}
	}
	power := func(watts float64) types.BestEffort {
		return types.BestEffort{Sport: "running", Kind: types.EffortPower, Target: 1200, Value: watts}
	}
	run := func(day int, efforts ...types.BestEffort) uuid.UUID {
		t.Helper()
		a := Activity{ID: uuid.New(), UserID: user.ID, Title: "run", Sport: "running", Distance: 10000,
			Date: monday.AddDate(0, 0, day), BestEfforts: efforts}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	first := run(0, five(1500))              // nothing before it
	second := run(1, five(1450), power(250)) // a new best, beaten later
	third := run(2, five(1480), power(260))  // second of three on 5 km; the best power
	fourth := run(3, five(1400))             // the best, still
	fifth := run(4, five(1600))              // fifth: nothing
	other := Users{Username: "other", Email: "other@test.local"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	theirs := Activity{ID: uuid.New(), UserID: other.ID, Title: "theirs", Sport: "running", Date: monday,
		BestEfforts: types.BestEffortArray{five(1000)}}
	if err := db.Omit("User", "Equipment").Create(&theirs).Error; err != nil {
		t.Fatal(err)
	}

	// a segment: passed by the first and the third
	segment := Segment{UserID: user.ID, Name: "the hill", Sport: "running", Points: types.GpsPointArray{{}, {}},
		Distance: 800}
	if err := db.CreateSegment(&segment); err != nil {
		t.Fatal(err)
	}
	for id, elapsed := range map[uuid.UUID]float64{first: 300, third: 290} {
		if err := db.ReplaceSegmentEfforts(segment.ID, id, []SegmentEffort{{SegmentID: segment.ID, ActivityID: id,
			Elapsed: elapsed, StartOffset: 120}}); err != nil {
			t.Fatal(err)
		}
	}
	// 30 km a week: 10 km each, the third run completes it
	goal := Goal{UserID: user.ID, Name: "weekly 30", Metric: "distance", Target: 30000, Sports: SportList{"running"},
		Period: "week"}
	if err := db.Create(&goal).Error; err != nil {
		t.Fatal(err)
	}

	all, err := db.GetHighlights(user.ID, []uuid.UUID{first, second, third, fourth, fifth, theirs.ID}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID][]types.Highlight{}
	for _, h := range all {
		got[h.ActivityID] = append(got[h.ActivityID], h)
	}
	if len(got[first]) != 0 || len(got[fifth]) != 0 || len(got[theirs.ID]) != 0 {
		t.Errorf("first %v, fifth %v, another user's %v: want none", got[first], got[fifth], got[theirs.ID])
	}
	if h := got[second]; len(h) != 1 || h[0].Kind != types.HighlightEffort || h[0].Rank != 1 || h[0].Current ||
		h[0].Previous == nil || *h[0].Previous != 1500 {
		t.Errorf("second: %+v", h)
	}
	// the third: the goal, the best power (first power before it: second), its pass, second on 5 km
	if h := got[third]; len(h) != 4 {
		t.Fatalf("third: %+v", h)
	} else {
		if h[0].Kind != types.HighlightGoal || h[0].Value != 30000 || h[0].Name != "weekly 30" {
			t.Errorf("third, goal: %+v", h[0])
		}
		for _, x := range h[1:] {
			switch {
			case x.Kind == types.HighlightEffort && x.EffortKind == types.EffortPower:
				if x.Rank != 1 || !x.Current || *x.Previous != 250 {
					t.Errorf("third, power: %+v", x)
				}
			case x.Kind == types.HighlightSegment:
				if x.Rank != 1 || !x.Current || *x.Previous != 300 || x.Name != "the hill" || *x.SegmentID != segment.ID {
					t.Errorf("third, segment: %+v", x)
				}
			case x.Kind == types.HighlightEffort:
				if x.Rank != 2 || x.Current || *x.Previous != 1450 {
					t.Errorf("third, 5 km: %+v", x)
				}
			}
		}
	}
	if h := got[fourth]; len(h) != 1 || h[0].Rank != 1 || !h[0].Current || *h[0].Previous != 1450 {
		t.Errorf("fourth: %+v", h)
	}
	// the latest first
	if all[0].ActivityID != fourth {
		t.Errorf("first highlight of %s, want the fourth", all[0].ActivityID)
	}

	// the dashboard: the new bests and the goals only
	recent, err := db.GetRecentHighlights(user.ID, monday, 10, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range recent {
		if h.Rank != 1 {
			t.Errorf("recent %+v: not a new best", h)
		}
	}
	if len(recent) != 5 { // fourth's 5 km, third's goal, power and segment, second's 5 km
		t.Errorf("%d recent highlights: %+v", len(recent), recent)
	}
	if limited, _ := db.GetRecentHighlights(user.ID, monday, 2, time.UTC); len(limited) != 2 {
		t.Errorf("limited to 2: %d", len(limited))
	}
}

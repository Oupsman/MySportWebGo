package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

// northTrack is a straight trace going north from lat 45, a point every 10 m and every 2 s, over meters
func northTrack(meters int) (types.GpsPointArray, types.FloatArray) {
	var points types.GpsPointArray
	var times types.FloatArray
	for i := 0; i*10 <= meters; i++ {
		points = append(points, types.GpsPoint{Lat: 45 + float64(i*10)/111_195, Lon: 5})
		times = append(times, float64(2*i))
	}
	return points, times
}

// the segments matched with older rules are matched again at the start, most recently ridden first: their
// passes follow the activities as they are now, the version is recorded, and the metrics count them
func TestBackfillSegmentMatches(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "rematch", Email: "rematch@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	points, times := northTrack(2000)
	bounds := types.GpsPointArray{points[0], points[len(points)-1]}
	ride := func(sport string, daysAgo int) uuid.UUID {
		t.Helper()
		a := models.Activity{ID: uuid.New(), UserID: user.ID, Title: sport, Sport: sport, GpsPoints: points,
			GpsTimeStamps: times, GpsBounds: bounds, Date: time.Now().AddDate(0, 0, -daysAgo)}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	first, second := ride("cycling", 1), ride("cycling", 2)
	// was a ride when the segment was created, a run now: its pass goes
	run := ride("running", 3)

	source, err := db.GetSegmentTrack(first)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := NewSegment(source, 50, 150, "the climb")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSegment(&segment); err != nil {
		t.Fatal(err)
	}
	// matched by older rules: a pass for the run only, of a wrong time
	stale := []models.SegmentEffort{{SegmentID: segment.ID, ActivityID: run, Elapsed: 999}}
	if err := db.ReplaceSegmentEfforts(segment.ID, run, stale); err != nil {
		t.Fatal(err)
	}
	// up to date: left alone
	current, err := NewSegment(source, 20, 60, "up to date")
	if err != nil {
		t.Fatal(err)
	}
	current.MatchVersion = SegmentMatchVersion
	if err := db.CreateSegment(&current); err != nil {
		t.Fatal(err)
	}
	// a segment of a sport no activity has any more: all its passes go
	lonely, err := NewSegment(source, 100, 180, "lonely")
	if err != nil {
		t.Fatal(err)
	}
	lonely.Sport = "swimming"
	if err := db.CreateSegment(&lonely); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceSegmentEfforts(lonely.ID, first, []models.SegmentEffort{{SegmentID: lonely.ID, ActivityID: first, Elapsed: 1}}); err != nil {
		t.Fatal(err)
	}

	ids, err := db.GetSegmentsBelowMatchVersion(SegmentMatchVersion)
	if err != nil {
		t.Fatal(err)
	}
	// lonely, whose pass is of yesterday, before the climb, whose pass is of 3 days ago
	if len(ids) != 2 || ids[0] != lonely.ID || ids[1] != segment.ID {
		t.Fatalf("segments to match again %v, want lonely (ridden yesterday) then the climb", ids)
	}

	// stopped before it starts: nothing done, done at the next start
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if err := BackfillSegmentMatches(stopped, db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if again, _ := db.GetSegmentsBelowMatchVersion(SegmentMatchVersion); len(again) != 2 {
		t.Fatalf("stopped: %d segments left, want 2", len(again))
	}

	matches := func() float64 {
		return testutil.ToFloat64(metrics.SegmentMatches.WithLabelValues(string(MatchBackfill)))
	}
	passes := func() float64 {
		return testutil.ToFloat64(metrics.SegmentPasses.WithLabelValues(string(MatchBackfill)))
	}
	matchesBefore, passesBefore := matches(), passes()
	ctx := WithBackfillStep(context.Background(), "match segments again (test)")
	if err := BackfillSegmentMatches(ctx, db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}

	efforts, err := db.GetSegmentEfforts(user.ID, segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := map[uuid.UUID]bool{}
	for _, e := range efforts {
		found[e.ActivityID] = true
		if e.Elapsed < 150 || e.Elapsed > 250 {
			t.Errorf("pass of %s: %v s, want about 200", e.ActivityID, e.Elapsed)
		}
	}
	if len(efforts) != 2 || !found[first] || !found[second] {
		t.Errorf("passes on the climb %+v, want the two rides", efforts)
	}
	if left, _ := db.GetSegmentEfforts(user.ID, lonely.ID); len(left) != 0 {
		t.Errorf("lonely: %d passes left", len(left))
	}
	if again, _ := db.GetSegmentsBelowMatchVersion(SegmentMatchVersion); len(again) != 0 {
		t.Errorf("%d segments still to match again", len(again))
	}
	if own, _ := db.GetSegmentEfforts(user.ID, current.ID); len(own) != 0 {
		t.Errorf("the segment up to date was matched again: %d passes", len(own))
	}
	// the two rides on the climb; none for lonely
	if got := matches() - matchesBefore; got != 2 {
		t.Errorf("%v matchings counted, want 2", got)
	}
	if got := passes() - passesBefore; got != 2 {
		t.Errorf("%v passes counted, want 2", got)
	}
	if got := testutil.ToFloat64(metrics.BackfillRemaining.WithLabelValues("match segments again (test)")); got != 0 {
		t.Errorf("%v segments remaining at the end", got)
	}

	// an activity analysed again is matched with every segment of its sport around
	reanalysed := func() float64 {
		return testutil.ToFloat64(metrics.SegmentMatches.WithLabelValues(string(MatchReanalysis)))
	}
	before := reanalysed()
	if err := MatchActivitySegments(db, second, MatchReanalysis); err != nil {
		t.Fatal(err)
	}
	if got := reanalysed() - before; got != 2 {
		t.Errorf("%v matchings of the re-analysed ride, want the climb and up to date", got)
	}
	if own, _ := db.GetActivitySegmentEfforts(user.ID, second); len(own) != 2 {
		t.Errorf("passes of the re-analysed ride: %d, want 2", len(own))
	}
}

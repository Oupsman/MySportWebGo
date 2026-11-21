package activityService

import (
	"MySportWeb/internal/pkg/models"
	"math"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// a segment cut from a real ride is found in the other rides of the same route, ranked; its passes follow
// the trims and the deletions, and stay the user's
func TestSegmentsOnRealRides(t *testing.T) {
	db := testDB(t)
	prototypes, err := filepath.Abs("../../prototypes")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir()) // the thumbnails drawn again by the trim
	user := models.Users{Username: "segments", Email: "segments@test.local"}
	stranger := models.Users{Username: "stranger", Email: "stranger@test.local"}
	for _, u := range []*models.Users{&user, &stranger} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID), IsDefault: true}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}
	create := func(name string) uuid.UUID {
		t.Helper()
		a, err := SumAnalyze(filepath.Join(prototypes, name+".fit"), user, equipment)
		if err != nil {
			t.Fatal(err)
		}
		a.ID, a.FilePath = uuid.New(), filepath.Join(prototypes, name+".fit")
		if err := db.CreateActivity(&a); err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	var rides []uuid.UUID
	for _, name := range sameRouteFiles {
		rides = append(rides, create(name))
	}
	elsewhere := create(otherRouteFile)

	// the middle third of the first ride
	source, err := db.GetSegmentTrack(rides[0])
	if err != nil {
		t.Fatal(err)
	}
	n := len(source.GpsPoints)
	segment, err := NewSegment(source, n/3, 2*n/3, "the middle")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSegment(&segment); err != nil {
		t.Fatal(err)
	}
	total, err := MatchSegmentActivities(db, segment)
	if err != nil {
		t.Fatal(err)
	}
	efforts, err := db.GetSegmentEfforts(user.ID, segment.ID)
	if err != nil || len(efforts) != total {
		t.Fatalf("%d efforts, %d found (%v)", len(efforts), total, err)
	}
	t.Logf("segment of %.0f m, %.0f m climbed: %d passes in %d rides of the route", segment.Distance,
		segment.ElevationGain, total, len(rides))
	passes := map[uuid.UUID]int{}
	for i, e := range efforts {
		passes[e.ActivityID]++
		if e.Rank < 1 || (i > 0 && e.Elapsed < efforts[i-1].Elapsed) || e.Efforts != total || e.SegmentName != "the middle" {
			t.Errorf("effort %d: %+v", i, e)
		}
		t.Logf("  %s: %.0f s, rank %d, heart rate %v", e.ActivityID, e.Elapsed, e.Rank, deref(e.AvgHeartRate))
	}
	if passes[rides[0]] != 1 {
		t.Fatalf("the ride it comes from: %d passes", passes[rides[0]])
	}
	want := source.GpsTimeStamps[2*n/3] - source.GpsTimeStamps[n/3]
	for _, e := range efforts {
		if e.ActivityID == rides[0] && math.Abs(e.Elapsed-want) > 2 {
			t.Errorf("the ride it comes from: %v s, want %v", e.Elapsed, want)
		}
	}
	// they all ride it, some with the GPS drifting 45 m off the source for 70 m
	if len(passes) != len(rides) {
		t.Errorf("found in %d rides of the route out of %d", len(passes), len(rides))
	}
	if passes[elsewhere] != 0 {
		t.Error("a pass in a ride of another route")
	}

	// the activity page lists its passes
	if own, err := db.GetActivitySegmentEfforts(user.ID, rides[0]); err != nil || len(own) != 1 || own[0].SegmentID != segment.ID {
		t.Errorf("passes of the activity: %+v (%v)", own, err)
	}
	// nothing for another user
	if _, err := db.GetSegment(stranger.ID, segment.ID); err == nil {
		t.Error("another user reads the segment")
	}
	if theirs, _ := db.GetSegmentEfforts(stranger.ID, segment.ID); len(theirs) != 0 {
		t.Error("another user reads the passes")
	}
	if found, _ := db.RenameSegment(stranger.ID, segment.ID, "mine"); found {
		t.Error("another user renames the segment")
	}
	if found, _ := db.DeleteSegment(stranger.ID, segment.ID); found {
		t.Error("another user deletes the segment")
	}

	// trimmed before the segment: its pass goes
	activity, err := db.GetActivity(rides[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TrimActivity(db, activity, source.GpsTimeStamps[n/3]+10, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if own, _ := db.GetActivitySegmentEfforts(user.ID, rides[0]); len(own) != 0 {
		t.Errorf("trimmed before the segment: %d passes left", len(own))
	}
	// an activity deleted: its passes with it
	for _, ride := range rides[1:] {
		if passes[ride] > 0 {
			if err := db.DeleteActivity(ride); err != nil {
				t.Fatal(err)
			}
			var left int64
			db.Model(&models.SegmentEffort{}).Where("activity_id = ?", ride).Count(&left)
			if left != 0 {
				t.Errorf("deleted activity: %d passes left", left)
			}
			break
		}
	}
	// the segment deleted: its passes with it
	if found, err := db.DeleteSegment(user.ID, segment.ID); err != nil || !found {
		t.Fatalf("deleting the segment: %v %v", found, err)
	}
	var left int64
	db.Model(&models.SegmentEffort{}).Where("segment_id = ?", segment.ID).Count(&left)
	if left != 0 {
		t.Errorf("deleted segment: %d passes left", left)
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

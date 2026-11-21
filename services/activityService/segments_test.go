package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

// metersToPoint returns the point x meters east and y meters north of a reference, near Grenoble
func metersToPoint(x, y float64) types.GpsPoint {
	const lat0, lon0 = 45.18, 5.72
	return types.GpsPoint{
		Lat: lat0 + y/earthRadiusMeters*180/math.Pi,
		Lon: lon0 + x/(earthRadiusMeters*math.Cos(lat0*math.Pi/180))*180/math.Pi,
	}
}

// ride returns a track through the points (meters), one GPS point every 5 m at 5 m/s, with noise (meters)
func ride(noise float64, waypoints ...[2]float64) models.SegmentTrack {
	rng := rand.New(rand.NewPCG(1, 2))
	track := models.SegmentTrack{Sport: "cycling"}
	t := 0.0
	add := func(x, y float64) {
		track.GpsPoints = append(track.GpsPoints, metersToPoint(x+noise*(rng.Float64()*2-1), y+noise*(rng.Float64()*2-1)))
		track.GpsTimeStamps = append(track.GpsTimeStamps, t)
		track.Hearts = append(track.Hearts, 140)
		track.HeartTimeStamps = append(track.HeartTimeStamps, t)
		t++
	}
	add(waypoints[0][0], waypoints[0][1])
	for i := 1; i < len(waypoints); i++ {
		a, b := waypoints[i-1], waypoints[i]
		length := math.Hypot(b[0]-a[0], b[1]-a[1])
		for d := 5.0; d <= length; d += 5 {
			add(a[0]+(b[0]-a[0])*d/length, a[1]+(b[1]-a[1])*d/length)
		}
	}
	return track
}

// a segment from 0 to 1000 m east, then 500 m north: an L
func lSegment(t *testing.T) models.Segment {
	t.Helper()
	source := ride(0, [2]float64{0, 0}, [2]float64{1000, 0}, [2]float64{1000, 500})
	segment, err := NewSegment(source, 0, len(source.GpsPoints)-1, "the L")
	if err != nil {
		t.Fatal(err)
	}
	return segment
}

func TestNewSegment(t *testing.T) {
	segment := lSegment(t)
	if math.Abs(segment.Distance-1500) > 5 {
		t.Errorf("distance %v, want 1500", segment.Distance)
	}
	if segment.Sport != "cycling" || segment.MinLat >= segment.MaxLat || segment.MinLon >= segment.MaxLon {
		t.Errorf("segment %+v", segment)
	}
	// one point every 5 m on 1500 m: the ones closer than segmentPointMeters dropped, never a gap
	if n := len(segment.Points); n < 150 || n > 302 {
		t.Errorf("%d points", n)
	}
	for i := 1; i < len(segment.Points); i++ {
		if gap := haversineMeters(segment.Points[i-1], segment.Points[i]); gap > 10.01 {
			t.Errorf("a gap of %.1f m at %d", gap, i)
		}
	}

	source := ride(0, [2]float64{0, 0}, [2]float64{1000, 0})
	for _, c := range []struct {
		start, end int
		name       string
		want       error
	}{
		{0, 10, "short", ErrSegmentLength}, // 50 m
		{10, 5, "backwards", ErrSegmentRange},
		{0, len(source.GpsPoints), "past the end", ErrSegmentRange},
		{-1, 100, "before the start", ErrSegmentRange},
		{0, 100, "", ErrSegmentNameSize},
		{0, 100, string(make([]rune, 101)), ErrSegmentNameSize},
	} {
		if _, err := NewSegment(source, c.start, c.end, c.name); err != c.want {
			t.Errorf("%d-%d %q: %v, want %v", c.start, c.end, c.name, err, c.want)
		}
	}
	if _, err := NewSegment(models.SegmentTrack{GpsPoints: source.GpsPoints}, 0, 100, "no times"); err != ErrSegmentNoTrace {
		t.Errorf("without time stamps: %v", err)
	}
}

// the climb of a segment: from the altitudes of the records over the stretch
func TestSegmentClimb(t *testing.T) {
	stamps := types.MultiString{"0", "1", "2", "3", "4", "5"}
	altitudes := types.FloatArray{100, 105, 103, 110, 120, 90}
	// from 1 to 4: 105 -> 103 is a descent, 103 -> 110 -> 120 climbs 17 m
	if got := climb(stamps, altitudes, 1, 4); got != 17 {
		t.Errorf("climb %v, want 17", got)
	}
	if got := climb(stamps, types.FloatArray{1, 2}, 0, 5); got != 0 {
		t.Errorf("misaligned series: %v", got)
	}
}

func TestMatchSegment(t *testing.T) {
	segment := lSegment(t)
	for _, c := range []struct {
		about     string
		track     models.SegmentTrack
		passes    int
		elapsedOK func(float64) bool
	}{
		{"the same way, longer before and after", ride(0, [2]float64{-500, 0}, [2]float64{1000, 0}, [2]float64{1000, 900}), 1,
			func(e float64) bool { return math.Abs(e-300) <= 2 }}, // 1500 m at 5 m/s
		{"with 8 m of GPS noise", ride(8, [2]float64{-500, 0}, [2]float64{1000, 0}, [2]float64{1000, 900}), 1,
			func(e float64) bool { return math.Abs(e-300) <= 10 }},
		{"backwards", ride(0, [2]float64{1000, 900}, [2]float64{1000, 0}, [2]float64{-500, 0}), 0, nil},
		{"a detour of 300 m in the middle", ride(0, [2]float64{-100, 0}, [2]float64{400, 0}, [2]float64{400, 300},
			[2]float64{700, 300}, [2]float64{700, 0}, [2]float64{1000, 0}, [2]float64{1000, 600}), 0, nil},
		{"turning off before the end", ride(0, [2]float64{-100, 0}, [2]float64{1000, 0}, [2]float64{1000, 200},
			[2]float64{1500, 200}), 0, nil},
		{"parallel, 100 m away", ride(0, [2]float64{-100, 100}, [2]float64{900, 100}, [2]float64{900, 600}), 0, nil},
		{"joining it in the middle", ride(0, [2]float64{500, -300}, [2]float64{500, 0}, [2]float64{1000, 0},
			[2]float64{1000, 600}), 0, nil},
		{"twice: out, around and again", ride(0, [2]float64{-100, 0}, [2]float64{1000, 0}, [2]float64{1000, 600},
			[2]float64{-100, 600}, [2]float64{-100, 0}, [2]float64{1000, 0}, [2]float64{1000, 600}), 2,
			func(e float64) bool { return math.Abs(e-300) <= 2 }},
	} {
		efforts := MatchSegment(segment, c.track)
		if len(efforts) != c.passes {
			t.Errorf("%s: %d passes, want %d", c.about, len(efforts), c.passes)
			continue
		}
		for _, e := range efforts {
			if !c.elapsedOK(e.Elapsed) {
				t.Errorf("%s: %v s", c.about, e.Elapsed)
			}
			if e.AvgHeartRate == nil || *e.AvgHeartRate != 140 || e.AvgPower != nil {
				t.Errorf("%s: heart rate %v, power %v", c.about, e.AvgHeartRate, e.AvgPower)
			}
		}
	}
}

// a loop segment starts and ends at the same place: each lap is a pass
func TestMatchLoopSegment(t *testing.T) {
	square := [][2]float64{{0, 0}, {300, 0}, {300, 300}, {0, 300}, {0, 0}}
	source := ride(0, square...)
	segment, err := NewSegment(source, 0, len(source.GpsPoints)-1, "the square")
	if err != nil {
		t.Fatal(err)
	}
	laps := [][2]float64{{-100, 0}}
	for range 3 {
		laps = append(laps, square[1:]...)
	}
	laps = append(laps, [2]float64{0, -200})
	efforts := MatchSegment(segment, ride(3, laps...))
	if len(efforts) != 3 {
		t.Fatalf("%d laps, want 3", len(efforts))
	}
	for i, e := range efforts {
		if math.Abs(e.Elapsed-240) > 4 { // 1200 m at 5 m/s
			t.Errorf("lap %d: %v s", i+1, e.Elapsed)
		}
		if i > 0 && e.StartOffset < efforts[i-1].StartOffset+efforts[i-1].Elapsed {
			t.Errorf("lap %d starts before the end of the previous one", i+1)
		}
	}
}

// a long segment with a long track: the matching stays fast
func BenchmarkMatchSegment(b *testing.B) {
	var waypoints [][2]float64
	for i := range 20 {
		waypoints = append(waypoints, [2]float64{float64(i) * 1000, float64(i%2) * 300})
	}
	source := ride(0, waypoints...)
	segment, err := NewSegment(source, 0, len(source.GpsPoints)-1, "long")
	if err != nil {
		b.Fatal(err)
	}
	track := ride(5, waypoints...)
	b.ResetTimer()
	for range b.N {
		if n := len(MatchSegment(segment, track)); n != 1 {
			b.Fatal(fmt.Sprint(n, " passes"))
		}
	}
}

// GPS drifting 45 m off the road for 70 m is still a pass; 70 m off for as long is not
func TestMatchSegmentDrift(t *testing.T) {
	segment := lSegment(t)
	drift := func(offset float64) models.SegmentTrack {
		return ride(0, [2]float64{-100, 0}, [2]float64{500, 0}, [2]float64{510, offset}, [2]float64{570, offset},
			[2]float64{580, 0}, [2]float64{1000, 0}, [2]float64{1000, 600})
	}
	if n := len(MatchSegment(segment, drift(45))); n != 1 {
		t.Errorf("45 m drift: %d passes, want 1", n)
	}
	if n := len(MatchSegment(segment, drift(90))); n != 0 {
		t.Errorf("a 90 m detour: %d passes, want 0", n)
	}
}

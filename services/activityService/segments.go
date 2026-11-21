package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"
)

// The segments: a stretch of a ride a user selects; the passes of their activities on it are compared.
const (
	// MinSegmentMeters and MaxSegmentMeters bound the length of a segment: shorter, GPS noise decides the
	// ranking; longer, it is a route
	MinSegmentMeters = 200.0
	MaxSegmentMeters = 100_000.0
	// SegmentEndpointMeters: a pass starts and ends within it of the start and the end of the segment
	SegmentEndpointMeters = 30.0
	// A pass follows the segment: SegmentMinCoverage of the segment within SegmentCorridorMeters of the pass
	// (GPS error, the width of a road), and SegmentMaxOutside at most beyond SegmentOuterMeters (a GPS jump);
	// the same for the pass along the segment (no detour). A stretch of GPS drifting 50 m off a road counts;
	// a detour, or a parallel road, goes farther than SegmentOuterMeters.
	SegmentCorridorMeters = 30.0
	SegmentMinCoverage    = 0.90
	SegmentOuterMeters    = 60.0
	SegmentMaxOutside     = 0.02
	// segmentSampleMeters: the segment is checked every that many meters
	segmentSampleMeters = 10.0
	// segmentPointMeters: the points of a segment closer than it to the previous one are dropped
	segmentPointMeters = 5.0
	// segmentStepMeters: the distance a pass travels counts the moves of at least that
	segmentStepMeters = 20.0
)

var (
	ErrSegmentRange    = errors.New("the stretch must be within the trace of the activity, its start before its end")
	ErrSegmentLength   = fmt.Errorf("a segment is between %.0f m and %.0f km long", MinSegmentMeters, MaxSegmentMeters/1000)
	ErrSegmentNoTrace  = errors.New("the activity has no GPS trace with its time stamps")
	ErrSegmentNameSize = errors.New("the name of a segment is 1 to 100 characters long")
)

// NewSegment builds the segment of the stretch of an activity between its GPS points start and end
// (included): its trace, its length, its climb from the altitudes of the activity over the stretch.
func NewSegment(activity models.SegmentTrack, start, end int, name string) (models.Segment, error) {
	if len([]rune(name)) == 0 || len([]rune(name)) > 100 {
		return models.Segment{}, ErrSegmentNameSize
	}
	points := activity.GpsPoints
	if len(points) < 2 || len(activity.GpsTimeStamps) != len(points) {
		return models.Segment{}, ErrSegmentNoTrace
	}
	if start < 0 || end >= len(points) || start >= end {
		return models.Segment{}, ErrSegmentRange
	}
	trace := types.GpsPointArray{points[start]}
	for _, p := range points[start+1 : end] {
		if haversineMeters(trace[len(trace)-1], p) >= segmentPointMeters {
			trace = append(trace, p)
		}
	}
	trace = append(trace, points[end])
	distance := traceLength(trace)
	if distance < MinSegmentMeters || distance > MaxSegmentMeters {
		return models.Segment{}, ErrSegmentLength
	}
	segment := models.Segment{
		UserID: activity.UserID, Name: name, Sport: activity.Sport, Points: trace, Distance: math.Round(distance),
		ElevationGain:    climb(activity.TimeStamps, activity.Altitudes, activity.GpsTimeStamps[start], activity.GpsTimeStamps[end]),
		SourceActivityID: &activity.ID,
	}
	segment.MinLat, segment.MinLon, segment.MaxLat, segment.MaxLon = bounds(trace)
	return segment, nil
}

// MatchSegment returns the passes of an activity on a segment, in their order: each starts near the start of
// the segment, ends near its end after about its length, and follows it in between
func MatchSegment(segment models.Segment, activity models.SegmentTrack) []models.SegmentEffort {
	trace, times := activity.GpsPoints, activity.GpsTimeStamps
	if len(segment.Points) < 2 || len(trace) < 2 || len(times) != len(trace) {
		return nil
	}
	ref := segment.Points[0].Lat
	seg := project(segment.Points, ref)
	pass := project(trace, ref)
	start, end := seg[0], seg[len(seg)-1]
	samples := resample(seg, segmentSampleMeters)

	var efforts []models.SegmentEffort
	for k := 0; k < len(pass); k++ {
		if dist(pass[k], start) > SegmentEndpointMeters {
			continue
		}
		// the closest point of this passage near the start
		first := closestInRun(pass, k, start)
		last, ok := passEnd(pass, first, end, segment.Distance)
		if !ok || !follows(pass[first:last+1], seg, samples) {
			// a later approach of the start may be the good one: after this one
			for k < len(pass)-1 && dist(pass[k+1], start) <= SegmentEndpointMeters {
				k++
			}
			continue
		}
		effort := models.SegmentEffort{SegmentID: segment.ID, ActivityID: activity.ID,
			StartOffset: times[first], Elapsed: times[last] - times[first]}
		if effort.Elapsed > 0 {
			effort.AvgHeartRate = averageOver(activity.HeartTimeStamps, uint16s(activity.Hearts), times[first], times[last])
			effort.AvgPower = averageOver(stamps(activity.PowerTS), uint16s(activity.Powers), times[first], times[last])
			efforts = append(efforts, effort)
		}
		// the next pass (a lap) starts after this one
		k = last
	}
	return efforts
}

// closestInRun returns, among the points from k on that stay within SegmentEndpointMeters of target, the
// closest one
func closestInRun(pass []point2D, k int, target point2D) int {
	best := k
	for i := k; i < len(pass) && dist(pass[i], target) <= SegmentEndpointMeters; i++ {
		if dist(pass[i], target) < dist(pass[best], target) {
			best = i
		}
	}
	return best
}

// passEnd returns where a pass from first reaches end: the closest point to end of the first approach after
// half the length of the segment (a loop segment starts and ends at the same place), before 1.5 times it
func passEnd(pass []point2D, first int, end point2D, length float64) (int, bool) {
	// the distance travelled counts the moves of segmentStepMeters at least: point to point, the GPS
	// jitter of a few meters would add up to much more than the distance ridden
	travelled, anchor := 0.0, pass[first]
	for i := first + 1; i < len(pass); i++ {
		if step := dist(anchor, pass[i]); step >= segmentStepMeters {
			travelled += step
			anchor = pass[i]
		}
		if travelled > 1.5*length+2*SegmentEndpointMeters {
			return 0, false
		}
		if travelled >= length/2 && dist(pass[i], end) <= SegmentEndpointMeters {
			return closestInRun(pass, i, end), true
		}
	}
	return 0, false
}

// follows tells whether a pass follows the segment, see SegmentMinCoverage: the samples of the segment along
// the pass, and the points of the pass along the segment
func follows(pass, seg, samples []point2D) bool {
	if len(pass) < 2 {
		return false
	}
	along := func(points, line []point2D) bool {
		return nearShare(points, line, SegmentCorridorMeters) >= SegmentMinCoverage &&
			nearShare(points, line, SegmentOuterMeters) >= 1-SegmentMaxOutside
	}
	return along(samples, pass) && along(pass, seg)
}

// nearShare returns the share of the points within corridor meters of the line. The points and the line
// go the same way: the line is searched around where the previous point was near it first, the whole of it
// only when that fails.
func nearShare(points, line []point2D, corridor float64) float64 {
	near, last := 0, 0
	nearLine := func(p point2D, from, to int) int {
		for j := from; j < to; j++ {
			if segmentDistance(p, line[j], line[j+1]) <= corridor {
				return j
			}
		}
		return -1
	}
	for _, p := range points {
		j := nearLine(p, max(0, last-nearShareBack), min(len(line)-1, last+nearShareAhead))
		if j < 0 {
			j = nearLine(p, 0, len(line)-1)
		}
		if j >= 0 {
			near++
			last = j
		}
	}
	return float64(near) / float64(len(points))
}

// the window of nearShare around the previous match, in segments of the line
const (
	nearShareBack  = 20
	nearShareAhead = 200
)

// resample returns points every step meters along a line, its ends included
func resample(line []point2D, step float64) []point2D {
	samples := []point2D{line[0]}
	carried := 0.0
	for i := 1; i < len(line); i++ {
		a, b := line[i-1], line[i]
		length := dist(a, b)
		for t := step - carried; t < length; t += step {
			samples = append(samples, point2D{X: a.X + (b.X-a.X)*t/length, Y: a.Y + (b.Y-a.Y)*t/length})
		}
		carried = math.Mod(carried+length, step)
	}
	return append(samples, line[len(line)-1])
}

func dist(a, b point2D) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}

func haversineMeters(a, b types.GpsPoint) float64 {
	p := project([]types.GpsPoint{a, b}, a.Lat)
	return dist(p[0], p[1])
}

// traceLength returns the length of a trace, in meters
func traceLength(trace types.GpsPointArray) float64 {
	if len(trace) < 2 {
		return 0
	}
	p := project(trace, trace[0].Lat)
	length := 0.0
	for i := 1; i < len(p); i++ {
		length += dist(p[i-1], p[i])
	}
	return length
}

func bounds(trace types.GpsPointArray) (minLat, minLon, maxLat, maxLon float64) {
	minLat, minLon, maxLat, maxLon = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range trace {
		minLat, maxLat = math.Min(minLat, p.Lat), math.Max(maxLat, p.Lat)
		minLon, maxLon = math.Min(minLon, p.Lon), math.Max(maxLon, p.Lon)
	}
	return
}

// climb returns the meters climbed between two times, from the altitudes of the records
func climb(timeStamps types.MultiString, altitudes types.FloatArray, from, to float64) float64 {
	times := stamps(timeStamps)
	if len(times) != len(altitudes) {
		return 0
	}
	gain, previous := 0.0, math.NaN()
	for i, t := range times {
		if t < from || t > to {
			continue
		}
		if !math.IsNaN(previous) && altitudes[i] > previous {
			gain += altitudes[i] - previous
		}
		previous = altitudes[i]
	}
	return math.Round(gain)
}

// averageOver returns the average of the values whose times are within [from, to], nil without any (or
// without any above 0: no sensor)
func averageOver(times []float64, values []float64, from, to float64) *float64 {
	if len(times) != len(values) {
		return nil
	}
	sum, n := 0.0, 0
	for i, t := range times {
		if t >= from && t <= to && values[i] > 0 {
			sum += values[i]
			n++
		}
	}
	if n == 0 {
		return nil
	}
	average := math.Round(sum / float64(n))
	return &average
}

func uint16s(values types.Uint16Array) []float64 {
	floats := make([]float64, len(values))
	for i, v := range values {
		floats[i] = float64(v)
	}
	return floats
}

// stamps parses time stamps stored as text, nil when one isn't a number
func stamps(texts types.MultiString) []float64 {
	times := make([]float64, len(texts))
	for i, text := range texts {
		t, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil
		}
		times[i] = t
	}
	return times
}

// MatchActivitySegments finds the passes of an activity on the segments of its user, replacing the ones it
// had: after its upload, its trim
func MatchActivitySegments(db *models.DB, activityID uuid.UUID) error {
	track, err := db.GetSegmentTrack(activityID)
	if err != nil {
		return err
	}
	// all its passes are replaced: a trim may have cut one of a segment it doesn't reach any more
	var efforts []models.SegmentEffort
	if len(track.GpsBounds) == 2 && len(track.GpsPoints) >= 2 {
		segments, err := db.GetSegmentsAround(track.UserID, track.Sport, track.GpsBounds[0], track.GpsBounds[1])
		if err != nil {
			return err
		}
		for _, segment := range segments {
			efforts = append(efforts, MatchSegment(segment, track)...)
		}
	}
	return db.ReplaceActivityEfforts(activityID, efforts)
}

// MatchSegmentActivities finds the passes of the activities of the user of a segment on it: at its creation.
// It returns how many there are.
func MatchSegmentActivities(db *models.DB, segment models.Segment) (int, error) {
	ids, err := db.GetActivitiesAround(segment.UserID, segment.Sport,
		types.GpsPoint{Lat: segment.MinLat, Lon: segment.MinLon}, types.GpsPoint{Lat: segment.MaxLat, Lon: segment.MaxLon})
	if err != nil {
		return 0, err
	}
	total := 0
	for _, id := range ids {
		track, err := db.GetSegmentTrack(id)
		if err != nil {
			return total, err
		}
		efforts := MatchSegment(segment, track)
		if err := db.ReplaceSegmentEfforts(segment.ID, id, efforts); err != nil {
			return total, err
		}
		total += len(efforts)
	}
	return total, nil
}

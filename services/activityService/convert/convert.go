// Package convert turns the GPX and TCX activities into FIT files: the server analyses FIT files only.
// The FIT file holds what the analysis reads, the records and a session whose totals are computed from
// them, so that a converted activity is trimmed, analysed again and recomputed as a recorded one.
package convert

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/kit/semicircles"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

// Format is the format of an activity file
type Format string

const (
	FIT     Format = "fit"
	GPX     Format = "gpx"
	TCX     Format = "tcx"
	Unknown Format = ""
)

const (
	// pauseGap is the longest time between two points counted as moving: a longer one is a pause (the
	// recorder stopped), as the timer of a FIT file stops
	pauseGap = 30 * time.Second
	// ascentThreshold is the change of altitude (m) counted in the ascent and the descent: below, the
	// noise of the altitudes would add up
	ascentThreshold = 3.0
	// maxSpeedWindow is the number of points the maximum speed is averaged over: the speed between two
	// GPS points jumps
	maxSpeedWindow = 5
	// MaxDecompressed is the largest activity file once decompressed
	MaxDecompressed = 200 << 20
)

// InvalidError is a file that can't be converted: not a GPX or TCX file, or without the points of an activity
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

func invalid(format string, args ...any) error {
	return &InvalidError{fmt.Errorf(format, args...)}
}

// Gunzip returns the content of a gzipped file, the content itself when it isn't gzipped
func Gunzip(content []byte) ([]byte, error) {
	if len(content) < 2 || content[0] != 0x1f || content[1] != 0x8b {
		return content, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, invalid("not a gzip file: %v", err)
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, MaxDecompressed+1))
	if err != nil {
		return nil, invalid("the gzip file is corrupted: %v", err)
	}
	if len(out) > MaxDecompressed {
		return nil, invalid("larger than %d MB decompressed", MaxDecompressed>>20)
	}
	return out, nil
}

// Detect returns the format of an activity file from its content (decompressed)
func Detect(content []byte) Format {
	// the header of a FIT file: its size (12 or 14), then ".FIT" at 8
	if len(content) >= 12 && (content[0] == 12 || content[0] == 14) && string(content[8:12]) == ".FIT" {
		return FIT
	}
	head := content[:min(len(content), 4096)]
	text := strings.ToLower(string(head))
	switch {
	case strings.Contains(text, "<gpx"):
		return GPX
	case strings.Contains(text, "<trainingcenterdatabase"):
		return TCX
	}
	return Unknown
}

// point is a point of a track, its fields nil when the file doesn't have them
type point struct {
	time        time.Time
	lat, lon    *float64 // degrees
	altitude    *float64 // m
	distance    *float64 // m since the start, given by the file
	speed       *float64 // m/s, given by the file
	heartRate   *float64
	cadence     *float64
	power       *float64
	temperature *float64
}

// track is what a GPX or TCX file holds of an activity
type track struct {
	sport    typedef.Sport // typedef.SportInvalid when the file doesn't say
	creator  string        // the application or the device that wrote it
	points   []point
	calories float64
}

// Result is a converted activity
type Result struct {
	FIT   []byte
	Sport typedef.Sport
	// SportFromFile tells whether the file named its sport, false when it is the one given to Convert
	SportFromFile bool
}

// Convert turns a GPX or TCX file (decompressed) into a FIT file. sport is the sport of a file that
// doesn't name it. An *InvalidError for a file that can't be converted.
func Convert(content []byte, sport typedef.Sport) (Result, error) {
	var t track
	var err error
	switch Detect(content) {
	case GPX:
		t, err = parseGPX(content)
	case TCX:
		t, err = parseTCX(content)
	default:
		return Result{}, invalid("not a FIT, GPX or TCX file")
	}
	if err != nil {
		return Result{}, err
	}
	result := Result{Sport: t.sport, SportFromFile: t.sport != typedef.SportInvalid}
	if !result.SportFromFile {
		result.Sport = sport
	}
	if result.Sport == typedef.SportInvalid {
		result.Sport = typedef.SportGeneric
	}
	t.sport = result.Sport
	activity, err := build(t)
	if err != nil {
		return Result{}, err
	}
	fit := activity.ToFIT(nil)
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(&fit); err != nil {
		return Result{}, fmt.Errorf("writing the FIT file: %w", err)
	}
	result.FIT = buf.Bytes()
	return result, nil
}

// clean sorts the points by time, without the ones without time nor the repeated times
func clean(points []point) []point {
	kept := points[:0]
	for _, p := range points {
		if !p.time.IsZero() {
			kept = append(kept, p)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].time.Before(kept[j].time) })
	unique := kept[:0]
	for i, p := range kept {
		if i > 0 && p.time.Equal(kept[i-1].time) {
			continue
		}
		unique = append(unique, p)
	}
	return unique
}

// haversine returns the distance between two positions, in m
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371008.8
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(a)))
}

type stat struct {
	sum, max float64
	n        int
}

func (s *stat) add(v *float64) {
	if v == nil || *v <= 0 {
		return
	}
	s.sum += *v
	s.n++
	s.max = math.Max(s.max, *v)
}

func (s stat) avg() float64 {
	if s.n == 0 {
		return 0
	}
	return s.sum / float64(s.n)
}

func clampUint8(v float64) uint8   { return uint8(math.Min(math.Round(v), 254)) }
func clampUint16(v float64) uint16 { return uint16(math.Min(math.Round(v), 65534)) }

// build makes the FIT activity of a track: its records, one lap and one session with the totals, the
// timer events of its pauses
func build(t track) (*filedef.Activity, error) {
	points := clean(t.points)
	if len(points) < 2 {
		return nil, invalid("the file has less than 2 points with a time: not an activity")
	}

	// the distance since the start: the file's when it gives it, else from the positions
	distances := make([]float64, len(points))
	fromFile := points[len(points)-1].distance != nil
	var lastLat, lastLon *float64
	for i, p := range points {
		switch {
		case fromFile && p.distance != nil:
			distances[i] = *p.distance
		case i > 0:
			distances[i] = distances[i-1]
			if !fromFile && p.lat != nil && lastLat != nil {
				distances[i] += haversine(*lastLat, *lastLon, *p.lat, *p.lon)
			}
		}
		if i > 0 && distances[i] < distances[i-1] {
			distances[i] = distances[i-1] // a distance never goes back
		}
		if p.lat != nil {
			lastLat, lastLon = p.lat, p.lon
		}
	}

	// the speed between two points, the file's when it gives it
	speeds := make([]float64, len(points))
	for i := 1; i < len(points); i++ {
		if s := points[i].speed; s != nil {
			speeds[i] = *s
			continue
		}
		if dt := points[i].time.Sub(points[i-1].time).Seconds(); dt > 0 && dt <= pauseGap.Seconds() {
			speeds[i] = (distances[i] - distances[i-1]) / dt
		}
	}
	if points[0].speed != nil {
		speeds[0] = *points[0].speed
	}

	start, end := points[0].time, points[len(points)-1].time
	activity := filedef.NewActivity()
	activity.FileId = *mesgdef.NewFileId(nil).
		SetType(typedef.FileActivity).
		SetManufacturer(typedef.ManufacturerInvalid).
		SetTimeCreated(start).
		SetProductName(strings.TrimSpace(t.creator))

	// the timer stops at each pause: the moving time is the time between two points closer than pauseGap
	timer := 0.0
	activity.Events = append(activity.Events, timerEvent(start, typedef.EventTypeStart))
	var hr, cadence, power stat
	var ascent, descent float64
	var reference *float64 // the altitude the next change is counted from
	maxSpeed := 0.0
	for i, p := range points {
		if i > 0 {
			if dt := p.time.Sub(points[i-1].time); dt <= pauseGap {
				timer += dt.Seconds()
			} else {
				activity.Events = append(activity.Events, timerEvent(points[i-1].time, typedef.EventTypeStopAll),
					timerEvent(p.time, typedef.EventTypeStart))
			}
		}
		record := mesgdef.NewRecord(nil).SetTimestamp(p.time).SetDistanceScaled(distances[i]).
			SetEnhancedSpeedScaled(speeds[i])
		if p.lat != nil {
			record.SetPositionLat(semicircles.ToSemicircles(*p.lat)).SetPositionLong(semicircles.ToSemicircles(*p.lon))
		}
		if p.altitude != nil {
			record.SetEnhancedAltitudeScaled(*p.altitude)
			if reference == nil {
				reference = p.altitude
			} else if change := *p.altitude - *reference; math.Abs(change) >= ascentThreshold {
				if change > 0 {
					ascent += change
				} else {
					descent -= change
				}
				reference = p.altitude
			}
		}
		if p.heartRate != nil && *p.heartRate > 0 {
			record.SetHeartRate(clampUint8(*p.heartRate))
		}
		if p.cadence != nil && *p.cadence >= 0 {
			record.SetCadence(clampUint8(*p.cadence))
		}
		if p.power != nil && *p.power >= 0 {
			record.SetPower(clampUint16(*p.power))
		}
		if p.temperature != nil {
			record.SetTemperature(int8(math.Max(-127, math.Min(127, math.Round(*p.temperature)))))
		}
		hr.add(p.heartRate)
		cadence.add(p.cadence)
		power.add(p.power)
		if i >= maxSpeedWindow-1 {
			window := 0.0
			for _, s := range speeds[i-maxSpeedWindow+1 : i+1] {
				window += s
			}
			maxSpeed = math.Max(maxSpeed, window/maxSpeedWindow)
		}
		activity.Records = append(activity.Records, record)
	}
	activity.Events = append(activity.Events, timerEvent(end, typedef.EventTypeStopAll))
	if len(points) < maxSpeedWindow {
		for _, s := range speeds {
			maxSpeed = math.Max(maxSpeed, s)
		}
	}

	elapsed := end.Sub(start).Seconds()
	distance := distances[len(distances)-1]
	avgSpeed := 0.0
	if timer > 0 {
		avgSpeed = distance / timer
	}
	lap := mesgdef.NewLap(nil).SetStartTime(start).SetTimestamp(end).SetSport(t.sport).
		SetTotalElapsedTimeScaled(elapsed).SetTotalTimerTimeScaled(timer).SetTotalDistanceScaled(distance)
	activity.Laps = append(activity.Laps, lap)

	session := mesgdef.NewSession(nil).
		SetStartTime(start).SetTimestamp(end).
		SetEvent(typedef.EventSession).SetEventType(typedef.EventTypeStop).
		SetSport(t.sport).SetSubSport(typedef.SubSportGeneric).
		SetTotalElapsedTimeScaled(elapsed).SetTotalTimerTimeScaled(timer).
		SetTotalDistanceScaled(distance).
		SetEnhancedAvgSpeedScaled(avgSpeed).SetEnhancedMaxSpeedScaled(maxSpeed).
		SetTotalAscent(clampUint16(ascent)).SetTotalDescent(clampUint16(descent)).
		SetNumLaps(1)
	for _, p := range points {
		if p.lat != nil {
			session.SetStartPositionLat(semicircles.ToSemicircles(*p.lat)).
				SetStartPositionLong(semicircles.ToSemicircles(*p.lon))
			break
		}
	}
	if hr.n > 0 {
		session.SetAvgHeartRate(clampUint8(hr.avg())).SetMaxHeartRate(clampUint8(hr.max))
	}
	if cadence.n > 0 {
		session.SetAvgCadence(clampUint8(cadence.avg())).SetMaxCadence(clampUint8(cadence.max))
	}
	if power.n > 0 {
		session.SetAvgPower(clampUint16(power.avg())).SetMaxPower(clampUint16(power.max))
	}
	if t.calories > 0 {
		session.SetTotalCalories(clampUint16(t.calories))
	}
	activity.Sessions = append(activity.Sessions, session)

	activity.Activity = mesgdef.NewActivity(nil).SetTimestamp(end).SetTotalTimerTimeScaled(timer).
		SetNumSessions(1).SetType(typedef.ActivityManual).
		SetEvent(typedef.EventActivity).SetEventType(typedef.EventTypeStop)
	return activity, nil
}

func timerEvent(at time.Time, eventType typedef.EventType) *mesgdef.Event {
	return mesgdef.NewEvent(nil).SetTimestamp(at).SetEvent(typedef.EventTimer).SetEventType(eventType).SetEventGroup(0)
}

// sportAliases are the names of the sports in the GPX and TCX files, lowercase
var sportAliases = map[string]typedef.Sport{
	"cycling": typedef.SportCycling, "biking": typedef.SportCycling, "bike": typedef.SportCycling,
	"ride": typedef.SportCycling, "road_biking": typedef.SportCycling, "mountain_biking": typedef.SportCycling,
	"running": typedef.SportRunning, "run": typedef.SportRunning, "trail_running": typedef.SportRunning,
	"walking": typedef.SportWalking, "walk": typedef.SportWalking,
	"hiking": typedef.SportHiking, "hike": typedef.SportHiking,
	"swimming": typedef.SportSwimming, "swim": typedef.SportSwimming,
}

// sportOf returns the sport of a name of a file, SportInvalid when it isn't one
func sportOf(name string) typedef.Sport {
	name = strings.ToLower(strings.TrimSpace(name))
	if sport, ok := sportAliases[name]; ok {
		return sport
	}
	sport := typedef.SportFromString(name)
	if sport == typedef.SportAll || sport == typedef.SportGeneric {
		return typedef.SportInvalid
	}
	return sport
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/rs/zerolog"
)

// effortSport is what the best efforts of a sport are measured over
type effortSport struct {
	Distances []float64 `json:"distances"` // meters
	// MaxSpeed (m/s) between two records is a GPS jump: no effort spans it
	MaxSpeed float64 `json:"max_speed"`
}

// EffortSports are the sports with best efforts over distances. The sports with a power meter also get the
// best powers over PowerDurations, whatever their sport.
var EffortSports = map[string]effortSport{
	"running": {Distances: []float64{400, 1000, 1609.344, 5000, 10000, 21097.5, 42195}, MaxSpeed: 10},
	"cycling": {Distances: []float64{10000, 20000, 40000, 100000}, MaxSpeed: 30},
}

// PowerDurations are the durations (seconds) of the best powers
var PowerDurations = []float64{5, 30, 60, 300, 1200, 3600}

const (
	// powerMaxGap is the longest time (seconds) a power sample lasts: beyond, the device was paused or lost
	// the power meter, the power is 0
	powerMaxGap = 5
	// maxValidPower above which a sample is a power meter glitch, ignored
	maxValidPower = 3000
	// maxPowerSeconds bounds the power series of a session, a day
	maxPowerSeconds = 24 * 3600
)

// effortSamples are the records of one session: seconds since the start of the activity, cumulated
// distances (meters, NaN when the record has none), and powers (watts, -1 when the record has none)
type effortSamples struct {
	sport     string
	times     []float64
	distances []float64
	powers    []float64
}

// BestEfforts returns the best efforts of an analysed FIT activity, per session: the distance ones over the
// records with a distance, the power ones over the records with a power measured by a power meter (not the
// power estimated from the speed and the slope). Never nil, empty without any.
func BestEfforts(fit *filedef.Activity) types.BestEffortArray {
	efforts := types.BestEffortArray{}
	if len(fit.Sessions) == 0 {
		return efforts
	}
	start := fit.Sessions[0].StartTime
	samples := make(map[int]*effortSamples)
	cursor := sessionCursor{sessions: fit.Sessions}
	for _, record := range fit.Records {
		session := cursor.at(record.Timestamp)
		s, found := samples[cursor.index]
		if !found {
			s = &effortSamples{sport: SportName(session.Sport)}
			samples[cursor.index] = s
		}
		s.times = append(s.times, record.Timestamp.Sub(start).Seconds())
		distance := math.NaN()
		if record.Distance != basetype.Uint32Invalid {
			distance = record.DistanceScaled()
		}
		s.distances = append(s.distances, distance)
		power := -1.0
		if hasPowerMeter(session) && record.Power != basetype.Uint16Invalid {
			power = float64(record.Power)
		}
		s.powers = append(s.powers, power)
	}
	for i := range fit.Sessions {
		if s, found := samples[i]; found {
			efforts = mergeEfforts(efforts, s.efforts())
		}
	}
	return efforts
}

// StoredBestEfforts returns the distance best efforts of an activity from its stored series, for the
// activities whose FIT file can't be read again: nil for a multisport one, whose series don't tell the sport
// of their records, and no power ones, the stored power may be an estimation.
func StoredBestEfforts(activity models.Activity) types.BestEffortArray {
	if len(activity.Legs) > 0 || len(activity.TimeStamps) != len(activity.Distances) {
		return types.BestEffortArray{}
	}
	s := effortSamples{sport: activity.Sport}
	for i, stamp := range activity.TimeStamps {
		t, err := strconv.ParseFloat(stamp, 64)
		if err != nil {
			return types.BestEffortArray{}
		}
		s.times = append(s.times, t)
		// the analysis stores 0 for the records without distance
		distance := activity.Distances[i]
		if distance == 0 && i > 0 {
			distance = math.NaN()
		}
		s.distances = append(s.distances, distance)
	}
	return mergeEfforts(types.BestEffortArray{}, s.efforts())
}

// efforts returns the best efforts of the samples of a session
func (s *effortSamples) efforts() types.BestEffortArray {
	var efforts types.BestEffortArray
	if config, found := EffortSports[s.sport]; found {
		times, distances := distanceSeries(s.times, s.distances)
		for _, target := range config.Distances {
			if seconds, start, found := fastestOver(times, distances, target, config.MaxSpeed); found {
				efforts = append(efforts, types.BestEffort{Sport: s.sport, Kind: types.EffortDistance,
					Target: target, Value: round1(seconds), StartOffset: round1(start)})
			}
		}
	}
	if len(s.powers) > 0 {
		first, perSecond := powerSeries(s.times, s.powers)
		for _, duration := range PowerDurations {
			if watts, offset, found := bestPowerOver(perSecond, int(duration)); found {
				efforts = append(efforts, types.BestEffort{Sport: s.sport, Kind: types.EffortPower,
					Target: duration, Value: math.Round(watts), StartOffset: round1(first + float64(offset))})
			}
		}
	}
	return efforts
}

// mergeEfforts adds efforts to merged, keeping the best one of each sport, kind and target: an activity
// with two legs of the same sport (a duathlon) has one effort per target
func mergeEfforts(merged, efforts types.BestEffortArray) types.BestEffortArray {
	for _, effort := range efforts {
		replaced := false
		for i, kept := range merged {
			if kept.Sport != effort.Sport || kept.Kind != effort.Kind || kept.Target != effort.Target {
				continue
			}
			if better(effort, kept) {
				merged[i] = effort
			}
			replaced = true
			break
		}
		if !replaced {
			merged = append(merged, effort)
		}
	}
	return merged
}

// better tells whether a is a better effort than b of the same kind
func better(a, b types.BestEffort) bool {
	if a.Kind == types.EffortDistance {
		return a.Value < b.Value
	}
	return a.Value > b.Value
}

// distanceSeries returns the samples with a distance, the distances never going down: a record below the
// previous one (a glitch of the device) takes its distance
func distanceSeries(times, distances []float64) ([]float64, []float64) {
	var ts, ds []float64
	for i, d := range distances {
		if math.IsNaN(d) {
			continue
		}
		if n := len(ds); n > 0 {
			if times[i] <= ts[n-1] {
				continue
			}
			d = math.Max(d, ds[n-1])
		}
		ts = append(ts, times[i])
		ds = append(ds, d)
	}
	return ts, ds
}

// fastestOver returns the shortest time to cover target meters, and when it started (seconds since the
// start of the activity), false when the samples cover less. The start is interpolated between the two
// records around it; the time includes the pauses. No effort spans two records farther apart than maxSpeed
// allows: a GPS jump would make a fake record.
func fastestOver(times, distances []float64, target, maxSpeed float64) (float64, float64, bool) {
	n := len(distances)
	if n < 2 || distances[n-1]-distances[0] < target {
		return 0, 0, false
	}
	// jumps[k]: the number of steps up to the record k (from k-1 to k) faster than maxSpeed
	jumps := make([]int, n)
	for k := 1; k < n; k++ {
		jumps[k] = jumps[k-1]
		if (distances[k]-distances[k-1])/(times[k]-times[k-1]) > maxSpeed {
			jumps[k]++
		}
	}
	best, bestStart, found := math.Inf(1), 0.0, false
	i := 0
	for j := 1; j < n; j++ {
		// i: the last record at least target meters before j
		for i+1 < j && distances[j]-distances[i+1] >= target {
			i++
		}
		if distances[j]-distances[i] < target || jumps[j] != jumps[i] {
			continue
		}
		startDistance := distances[j] - target
		start := times[i]
		if span := distances[i+1] - distances[i]; span > 0 {
			start += (times[i+1] - times[i]) * (startDistance - distances[i]) / span
		}
		if elapsed := times[j] - start; elapsed > 0 && elapsed < best {
			best, bestStart, found = elapsed, start, true
		}
	}
	return best, bestStart, found
}

// powerSeries returns the power of each second from the first sample with a power (its time, seconds since
// the start of the activity, is returned too): the one of the last sample, 0 when it's more than powerMaxGap
// seconds old. Nil without any power.
func powerSeries(times, powers []float64) (float64, []float64) {
	var ts, ps []float64
	for i, p := range powers {
		if p < 0 || p > maxValidPower || (len(ts) > 0 && times[i] <= ts[len(ts)-1]) {
			continue
		}
		ts = append(ts, times[i])
		ps = append(ps, p)
	}
	if len(ts) == 0 {
		return 0, nil
	}
	first := ts[0]
	seconds := int(math.Min(ts[len(ts)-1]-first, maxPowerSeconds)) + 1
	perSecond := make([]float64, seconds)
	k := 0
	for s := range perSecond {
		t := first + float64(s)
		for k+1 < len(ts) && ts[k+1] <= t {
			k++
		}
		if t-ts[k] <= powerMaxGap {
			perSecond[s] = ps[k]
		}
	}
	return first, perSecond
}

// bestPowerOver returns the highest average power over duration seconds, and the second it starts at,
// false when the series is shorter or never above 0
func bestPowerOver(perSecond []float64, duration int) (float64, int, bool) {
	if duration <= 0 || len(perSecond) < duration {
		return 0, 0, false
	}
	var sum float64
	for _, p := range perSecond[:duration] {
		sum += p
	}
	best, bestStart := sum, 0
	for s := duration; s < len(perSecond); s++ {
		sum += perSecond[s] - perSecond[s-duration]
		if sum > best {
			best, bestStart = sum, s-duration+1
		}
	}
	if best <= 0 {
		return 0, 0, false
	}
	return best / float64(duration), bestStart, true
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}

// BackfillProgressInterval is how often a backfill logs how far it is
var BackfillProgressInterval = 30 * time.Second

// backfillProgress logs the progress of a backfill: at its start, then at most every
// BackfillProgressInterval, with an estimate of the time left
type backfillProgress struct {
	logger  zerolog.Logger
	total   int
	started time.Time
	logged  time.Time
}

func newBackfillProgress(logger zerolog.Logger, total int, msg string) *backfillProgress {
	now := time.Now()
	logger.Info().Int("activities", total).Msg(msg)
	return &backfillProgress{logger: logger, total: total, started: now, logged: now}
}

// done logs the progress after done activities, when the last log is old enough
func (p *backfillProgress) done(done int, fields func(*zerolog.Event) *zerolog.Event) {
	now := time.Now()
	if done >= p.total || now.Sub(p.logged) < BackfillProgressInterval {
		return
	}
	p.logged = now
	elapsed := now.Sub(p.started)
	left := time.Duration(float64(elapsed) / float64(done) * float64(p.total-done)).Round(time.Second)
	event := p.logger.Info().Int("done", done).Int("total", p.total).
		Str("percent", strconv.FormatFloat(float64(done)*100/float64(p.total), 'f', 1, 64)).
		Str("elapsed", elapsed.Round(time.Second).String()).Str("left", left.String())
	fields(event).Msg("backfill in progress")
}

// BackfillBestEfforts computes the best efforts of the activities analysed before they existed: their FIT
// file is analysed again; without readable file, the distance ones come from the stored series
func BackfillBestEfforts(db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutBestEfforts()
	if err != nil || len(ids) == 0 {
		return err
	}
	logger = logger.With().Str("backfill", "best_efforts").Logger()
	progress := newBackfillProgress(logger, len(ids), "computing the best efforts of activities")
	fromSeries := 0
	for i, id := range ids {
		activity, err := db.GetActivityDistances(id)
		if err != nil {
			return err
		}
		efforts, err := fileBestEfforts(activity)
		if err != nil {
			fromSeries++
			efforts = StoredBestEfforts(activity)
		}
		if err := db.SetActivityBestEfforts(id, efforts); err != nil {
			return err
		}
		progress.done(i+1, func(e *zerolog.Event) *zerolog.Event { return e.Int("without_file", fromSeries) })
	}
	logger.Info().Int("activities", len(ids)).Int("without_file", fromSeries).
		Str("elapsed", time.Since(progress.started).Round(time.Second).String()).Msg("best efforts computed")
	return nil
}

// fileBestEfforts analyses the FIT file of an activity again, as it was trimmed, for its best efforts,
// waiting for an analysis slot as long as the server is busy
func fileBestEfforts(activity models.Activity) (types.BestEffortArray, error) {
	for {
		// only the records are used: no resting heart rate nor altitude correction
		analysed, err := analyzeFile(activity.FilePath, activity.User, activity.TrimEnd, nil, nil)
		if errors.Is(err, ErrBusy) {
			time.Sleep(BusyRetryDelay)
			continue
		}
		if err != nil {
			return nil, err
		}
		return analysed.BestEfforts, nil
	}
}

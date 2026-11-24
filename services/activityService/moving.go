package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"math"
	"strconv"

	"github.com/rs/zerolog"
)

// The moving time: the time of the stretches covered faster than the moving speed of the sport. A device
// without auto-pause keeps its timer running at a stop (a red light, a break): its duration and its average
// speed count the stops, the moving ones don't.
const (
	// movingWindow is the shortest stretch whose speed is judged: the GPS drifting a few meters while
	// stopped would pass for a slow walk over a second
	movingWindow = 5.0 // seconds
	// defaultMovingSpeed is the moving speed of the sports without their own
	defaultMovingSpeed = 0.5 // m/s, 1.8 km/h
)

// movingSpeeds are the speeds under which an activity of the sport stands still, m/s
var movingSpeeds = map[string]float64{
	"cycling":  1.0, // 3.6 km/h
	"running":  0.7, // 2.5 km/h
	"walking":  0.3, // 1.1 km/h
	"hiking":   0.3,
	"swimming": 0.1,
}

// MovingTime returns the seconds spent moving of an activity of a sport, from its records: their seconds
// since the start and their distances (meters, cumulated). False when they don't tell (no distance).
func MovingTime(sport string, times, distances []float64) (float64, bool) {
	threshold, ok := movingSpeeds[sport]
	if !ok {
		threshold = defaultMovingSpeed
	}
	n := min(len(times), len(distances))
	moving, measured := 0.0, false
	from := -1 // the record the current stretch starts at, -1 before a valid one
	for i := 0; i < n; i++ {
		if math.IsNaN(times[i]) || math.IsNaN(distances[i]) {
			continue
		}
		if from < 0 {
			from = i
			continue
		}
		dt := times[i] - times[from]
		if dt < movingWindow && i < n-1 {
			continue
		}
		dd := distances[i] - distances[from]
		// a gap without records counts when its distance was covered: the GPS lost in a tunnel, not a stop
		if dt > 0 && dd/dt >= threshold {
			moving += dt
		}
		if dd > 0 {
			measured = true
		}
		from = i
	}
	return moving, measured
}

// stampSeconds reads the time stamps of the records, NaN for the unreadable ones
func stampSeconds(stamps types.MultiString) []float64 {
	seconds := make([]float64, len(stamps))
	for i, s := range stamps {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			v = math.NaN()
		}
		seconds[i] = v
	}
	return seconds
}

// movingOf returns the moving time and the average moving speed of an activity (its sport, the time stamps
// and distances of its records, its distance and duration): 0, 0 when its records don't tell. The moving time
// is at most its duration (the timer, which a device with auto-pause stops).
func movingOf(sport string, stamps types.MultiString, distances []float64, distance float64, duration uint32) (float64, float64) {
	moving, ok := MovingTime(sport, stampSeconds(stamps), distances)
	if !ok || moving <= 0 {
		return 0, 0
	}
	if duration > 0 && moving > float64(duration) {
		moving = float64(duration)
	}
	return math.Round(moving), distance / math.Round(moving)
}

// setMoving sets the moving time and speed of an activity analysed
func setMoving(activity *models.Activity) {
	moving, speed := movingOf(activity.Sport, activity.TimeStamps, activity.Distances, activity.Distance, activity.Duration)
	activity.MovingTime, activity.AvgMovingSpeed = &moving, &speed
}

// BackfillMovingTime computes the moving time of the activities analysed before it existed, from their
// stored series: their FIT file isn't read again
func BackfillMovingTime(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutMovingTime()
	if err != nil || len(ids) == 0 {
		return err
	}
	progress := newBackfillProgress(ctx, logger, "activities", len(ids), "computing the moving time of activities")
	for i, id := range ids {
		if ctx.Err() != nil {
			return nil
		}
		a, err := db.GetActivityMovingSeries(id)
		if err != nil {
			return err
		}
		moving, speed := movingOf(a.Sport, a.TimeStamps, a.Distances, a.Distance, a.Duration)
		if err := db.SetActivityMoving(id, moving, speed); err != nil {
			return err
		}
		progress.done(i+1, nil)
	}
	logger.Info().Int("activities", len(ids)).Str("elapsed", progress.elapsed()).Msg("moving times computed")
	return nil
}

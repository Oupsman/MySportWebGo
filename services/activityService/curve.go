package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"math"
	"time"

	"github.com/rs/zerolog"
)

// PowerCurveDurations are the durations (s) of the power curve of a ride, from 1 s to 5 h: about evenly
// spread on a log scale, with the ones of PowerDurations so that the curve meets the best efforts
var PowerCurveDurations = []float64{1, 2, 3, 5, 10, 15, 20, 30, 45, 60, 90, 120, 180, 240, 300, 360, 480, 600,
	720, 900, 1200, 1500, 1800, 2400, 3600, 4500, 5400, 7200, 9000, 10800, 14400, 18000}

// ComputePowerCurve returns the best power of a stream over PowerCurveDurations, computed as the best
// efforts (powerSeries: the pauses count as 0 W). Empty, not nil, without power or for the durations
// longer than the stream.
func ComputePowerCurve(times, powers []float64) types.PowerCurve {
	curve := types.PowerCurve{}
	if len(powers) == 0 {
		return curve
	}
	first, perSecond := powerSeries(times, powers)
	for _, duration := range PowerCurveDurations {
		watts, offset, found := bestPowerOver(perSecond, int(duration))
		if !found {
			break // the longer durations don't fit either
		}
		curve = append(curve, types.PowerCurvePoint{Duration: duration, Watts: math.Round(watts),
			StartOffset: round1(first + float64(offset))})
	}
	return curve
}

// setPowerCurve sets the power curve of an activity whose power was measured, of any sport (a ride, a run
// with a running power meter...): empty without, or with the power the analysis estimated on a ride without
// power meter, or for a multisport one (its legs share the stream)
func setPowerCurve(activity *models.Activity) {
	activity.PowerCurve = types.PowerCurve{}
	if len(activity.Powers) == 0 || len(activity.Legs) > 0 || !hasMeasuredPower(*activity) {
		return
	}
	times, powers := powerSamples(*activity)
	activity.PowerCurve = ComputePowerCurve(times, powers)
}

// BackfillPowerCurves computes the power curve of the activities analysed before it existed, from their
// stored power stream
func BackfillPowerCurves(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutPowerCurve()
	if err != nil || len(ids) == 0 {
		return err
	}
	logger = logger.With().Str("backfill", "power_curve").Logger()
	progress := newBackfillProgress(logger, len(ids), "computing the power curves of activities")
	for i, id := range ids {
		// stopping: what is done stays done, the rest goes on at the next start
		if ctx.Err() != nil {
			return nil
		}
		activity, err := db.GetActivityPowerStream(id)
		if err != nil {
			return err
		}
		setPowerCurve(&activity)
		if err := db.SetActivityPowerCurve(id, activity.PowerCurve); err != nil {
			return err
		}
		progress.done(i+1, nil)
	}
	logger.Info().Int("activities", len(ids)).Msg("power curves computed")
	return nil
}

const (
	// cpMinDuration and cpMaxDuration bound the points of the curve the critical power is fitted on: shorter,
	// the anaerobic power dominates; longer, the fatigue the 2-parameter model ignores
	cpMinDuration = 180
	cpMaxDuration = 1200
	// cpMinPoints is the fewest points of a fit, cpMinR2 how well the work must follow the model
	cpMinPoints = 3
	cpMinR2     = 0.98
)

// CriticalPower is the 2-parameter model of a power curve: the power that can be held a long time (W), and
// the work that can be done above it (J)
type CriticalPower struct {
	CP     float64 `json:"cp"`
	WPrime float64 `json:"w_prime"`
	R2     float64 `json:"r2"`
}

// FitCriticalPower fits the critical power on the points of a curve between cpMinDuration and
// cpMaxDuration: the work over a duration t is CP * t + W'. Nil without enough points, or when they don't
// follow the model (efforts that weren't all-out).
func FitCriticalPower(curve []models.PowerCurveBest) *CriticalPower {
	var ts, works []float64
	for _, p := range curve {
		if p.Duration >= cpMinDuration && p.Duration <= cpMaxDuration && p.Watts > 0 {
			ts = append(ts, p.Duration)
			works = append(works, p.Watts*p.Duration)
		}
	}
	if len(ts) < cpMinPoints {
		return nil
	}
	n := float64(len(ts))
	var sumT, sumW, sumTT, sumTW float64
	for i := range ts {
		sumT += ts[i]
		sumW += works[i]
		sumTT += ts[i] * ts[i]
		sumTW += ts[i] * works[i]
	}
	cp := (n*sumTW - sumT*sumW) / (n*sumTT - sumT*sumT)
	wPrime := (sumW - cp*sumT) / n
	meanW := sumW / n
	var residual, total float64
	for i := range ts {
		e := works[i] - (cp*ts[i] + wPrime)
		residual += e * e
		total += (works[i] - meanW) * (works[i] - meanW)
	}
	r2 := 1 - residual/total
	if cp <= 0 || wPrime <= 0 || r2 < cpMinR2 {
		return nil
	}
	return &CriticalPower{CP: math.Round(cp), WPrime: math.Round(wPrime), R2: math.Round(r2*1000) / 1000}
}

// PowerCurveBest is a point of the curve of a period, with the rider's weight at the date of its ride
type PowerCurveBest struct {
	models.PowerCurveBest
	WattsPerKg *float64 `json:"watts_per_kg"` // nil when the weight is unknown
}

// UserPowerCurve returns the power curve of the activities of a sport of a user between from (included) and
// to (excluded), nil for no bound, each point divided by the weight of the user at the date of its activity
func UserPowerCurve(db *models.DB, user models.Users, sport string, from, to *time.Time) ([]PowerCurveBest, *CriticalPower, error) {
	best, err := db.GetPowerCurve(user.ID, sport, from, to)
	if err != nil {
		return nil, nil, err
	}
	measures, err := db.GetWeightMeasures(user.ID)
	if err != nil {
		return nil, nil, err
	}
	curve := make([]PowerCurveBest, len(best))
	for i, p := range best {
		curve[i].PowerCurveBest = p
		if weight := riderWeightAt(measures, float64(user.Weight), p.Date); weight > 0 {
			perKg := math.Round(p.Watts/weight*100) / 100
			curve[i].WattsPerKg = &perKg
		}
	}
	return curve, FitCriticalPower(best), nil
}

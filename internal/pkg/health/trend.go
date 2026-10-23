// Package health estimates when a target weight is reached, from the trend of the recent measures
package health

import (
	"math"
	"time"
)

const (
	// Window is the period of the measures the trend is computed on, before now
	Window = 60 * 24 * time.Hour
	// MinMeasures and MinSpan: fewer measures, or measures closer in time, make no trend
	MinMeasures = 3
	MinSpan     = 14 * 24 * time.Hour
	// ReachedMargin is how close to the target the latest measure must be to count as reached, in kg
	ReachedMargin = 0.3
	// MaxHorizon: further than this, the trend is too slow to tell a date
	MaxHorizon = 3 * 365 * 24 * time.Hour
	// flat: less than this per week is no trend, in kg
	flat = 0.01
)

// The states of an estimate
const (
	NoTarget       = "no_target"
	NotEnoughData  = "not_enough_data"
	Reached        = "reached"
	OnTrack        = "on_track"
	WrongDirection = "wrong_direction"
	TooSlow        = "too_slow"
)

// Measure is a weight measured at a date
type Measure struct {
	Date   time.Time
	Weight float64 // kg
}

// Estimate is when the target weight is reached at the pace of the recent measures
type Estimate struct {
	Status   string     `json:"status"`
	Target   float64    `json:"target"`   // kg, 0 without target
	Current  float64    `json:"current"`  // the weight of the trend now, kg
	PerWeek  float64    `json:"per_week"` // the pace of the trend, kg per week, negative when losing
	Date     *time.Time `json:"date"`     // when the trend meets the target, only on track
	Measures int        `json:"measures"` // the measures of the trend
}

// EstimateWeight fits a line on the measures of the last Window (least squares) and tells when it meets
// target. The measures may be in any order; those without weight (0) are ignored.
func EstimateWeight(measures []Measure, target float64, now time.Time) Estimate {
	estimate := Estimate{Status: NoTarget, Target: target}
	if target <= 0 {
		return estimate
	}
	var recent []Measure
	var latest *Measure
	for i, m := range measures {
		if m.Weight <= 0 || m.Date.After(now) || now.Sub(m.Date) > Window {
			continue
		}
		recent = append(recent, m)
		if latest == nil || m.Date.After(latest.Date) {
			latest = &measures[i]
		}
	}
	estimate.Measures = len(recent)
	if latest == nil {
		estimate.Status = NotEnoughData
		return estimate
	}
	// what was measured last, not the trend: the user sees it on the scale
	gap := target - latest.Weight
	if math.Abs(gap) <= ReachedMargin {
		estimate.Status = Reached
		estimate.Current = latest.Weight
		return estimate
	}

	earliest := recent[0]
	for _, m := range recent {
		if m.Date.Before(earliest.Date) {
			earliest = m
		}
	}
	// the measures went across the target: reached, even if the scale went a bit further
	if (earliest.Weight-target > 0) != (latest.Weight-target > 0) {
		estimate.Status = Reached
		estimate.Current = latest.Weight
		return estimate
	}
	if len(recent) < MinMeasures || latest.Date.Sub(earliest.Date) < MinSpan {
		estimate.Status = NotEnoughData
		return estimate
	}

	// weight = intercept + slope * days since now, days as x keeps the numbers small
	var sumX, sumY, sumXX, sumXY float64
	for _, m := range recent {
		x := m.Date.Sub(now).Hours() / 24
		sumX += x
		sumY += m.Weight
		sumXX += x * x
		sumXY += x * m.Weight
	}
	n := float64(len(recent))
	slope := (n*sumXY - sumX*sumY) / (n*sumXX - sumX*sumX) // kg per day
	estimate.Current = (sumY - slope*sumX) / n             // the line at x = 0, now
	estimate.PerWeek = slope * 7

	// toward the target: losing when it is below the scale, gaining when it is above
	if math.Abs(estimate.PerWeek) < flat || (gap < 0) != (slope < 0) {
		estimate.Status = WrongDirection
		return estimate
	}
	remaining := target - estimate.Current
	// the line is already past the target while the scale isn't yet: any day now
	if (remaining < 0) != (gap < 0) {
		date := now
		estimate.Status, estimate.Date = OnTrack, &date
		return estimate
	}
	days := remaining / slope
	if time.Duration(days*24)*time.Hour > MaxHorizon {
		estimate.Status = TooSlow
		return estimate
	}
	date := now.Add(time.Duration(days * 24 * float64(time.Hour)))
	estimate.Status, estimate.Date = OnTrack, &date
	return estimate
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"math"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

// PowerZoneLimits are the lower limits of Coggan's zones 2 to 7, as fractions of the FTP: zone 1 (active
// recovery) holds everything below 56%, coasting included, zone 7 (neuromuscular) everything from 151%
var PowerZoneLimits = [models.PowerZones - 1]float64{0.56, 0.76, 0.91, 1.06, 1.21, 1.51}

const (
	// FTPWindowDays: the FTP estimated at a date comes from the best 20 minutes of the rides of the days before
	FTPWindowDays = 42
	// FTPDuration is the duration (s) of the best power the FTP is estimated from, FTPFactor of it
	FTPDuration = 1200
	FTPFactor   = 0.95
	// npWindow is the rolling average (s) of the normalized power
	npWindow = 30
	// minPowerSeconds: a shorter power stream has no normalized power nor TSS
	minPowerSeconds = 60
)

// FTPFunc returns the FTP (W) of a user at a date, own being the best 20 minutes of the activity analysed
// (0 without): it counts though the activity isn't saved yet. 0 when unknown.
type FTPFunc func(date time.Time, own float64) (uint16, error)

// EstimateFTP returns FTPFactor of a best 20 minutes power, in W
func EstimateFTP(best20 float64) uint16 {
	return uint16(math.Round(best20 * FTPFactor))
}

// UserFTP returns the FTP of a user: theirs when they set it, else estimated from their best 20 minutes over
// the FTPWindowDays before the date
func UserFTP(db *models.DB, userID uint) FTPFunc {
	return func(date time.Time, own float64) (uint16, error) {
		set, err := db.GetUserFTP(userID)
		if err != nil || set > 0 {
			return set, err
		}
		best, err := db.GetBestPower(userID, FTPDuration, date.AddDate(0, 0, -FTPWindowDays), date)
		if err != nil {
			return 0, err
		}
		if best != nil {
			own = math.Max(own, best.Value)
		}
		return EstimateFTP(own), nil
	}
}

// ownBest20 returns the best 20 minutes power of a ride among its best efforts, 0 without
func ownBest20(efforts types.BestEffortArray) float64 {
	for _, e := range efforts {
		if e.Sport == models.FTPSport && e.Kind == types.EffortPower && e.Target == FTPDuration {
			return e.Value
		}
	}
	return 0
}

// hasMeasuredPower tells whether the power stream of an activity was measured by a power meter: the analysis
// also stores the power it estimates on the rides without one, but only measured power has best efforts
func hasMeasuredPower(activity models.Activity) bool {
	for _, e := range activity.BestEfforts {
		if e.Kind == types.EffortPower {
			return true
		}
	}
	return false
}

// powerSamples returns the power stream of an activity, measured or estimated: see hasMeasuredPower: the times (s since the start) and the watts
func powerSamples(activity models.Activity) ([]float64, []float64) {
	n := min(len(activity.Powers), len(activity.PowerTS))
	times := make([]float64, 0, n)
	powers := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		t, err := strconv.ParseFloat(activity.PowerTS[i], 64)
		if err != nil || float64(activity.Powers[i]) > maxValidPower {
			continue
		}
		times = append(times, t)
		powers = append(powers, float64(activity.Powers[i]))
	}
	return times, powers
}

// PowerMetrics are the power metrics of a ride
type PowerMetrics struct {
	Zones           types.FloatArray  // seconds in each zone
	Bounds          types.Uint16Array // W: 0, then the lower limits of the zones 2 to 7
	NormalizedPower float64           // W, 0 when the stream is shorter than minPowerSeconds
	Intensity       float64
	TSS             float64
}

// ComputePowerMetrics returns the power metrics of a stream with an FTP. As the heart rate zones, each
// interval counts at the power of its end, the gaps longer than powerMaxGap (pauses, drops) are skipped.
func ComputePowerMetrics(times, powers []float64, ftp uint16) PowerMetrics {
	m := PowerMetrics{Zones: make(types.FloatArray, models.PowerZones)}
	if ftp == 0 || len(powers) < 2 {
		return m
	}
	m.Bounds = types.Uint16Array{0}
	for _, limit := range PowerZoneLimits {
		m.Bounds = append(m.Bounds, uint16(math.Round(limit*float64(ftp))))
	}
	// the power of each second recorded, the pauses left out: the normalized power of the time ridden
	var perSecond []float64
	for i := 1; i < len(powers); i++ {
		dt := times[i] - times[i-1]
		if dt <= 0 || dt > powerMaxGap {
			continue
		}
		m.Zones[powerZone(powers[i], m.Bounds)] += dt
		for k := 0; k < int(math.Round(dt)); k++ {
			perSecond = append(perSecond, powers[i])
		}
	}
	for i := range m.Zones {
		m.Zones[i] = math.Round(m.Zones[i]*10) / 10
	}
	if len(perSecond) < minPowerSeconds {
		return m
	}
	// the fourth root of the mean of the fourth powers of the 30 s rolling average
	sum, fourth := 0.0, 0.0
	for i, p := range perSecond {
		sum += p
		if i >= npWindow {
			sum -= perSecond[i-npWindow]
		}
		if i >= npWindow-1 {
			fourth += math.Pow(sum/npWindow, 4)
		}
	}
	np := math.Pow(fourth/float64(len(perSecond)-npWindow+1), 0.25)
	intensity := np / float64(ftp)
	m.NormalizedPower = math.Round(np*10) / 10
	m.Intensity = math.Round(intensity*1000) / 1000
	m.TSS = math.Round(float64(len(perSecond))*intensity*intensity/36*10) / 10
	return m
}

// powerZone returns the index of the zone of a power, compared to the bounds in W the user is shown
func powerZone(power float64, bounds types.Uint16Array) int {
	zone := 0
	for zone < len(PowerZoneLimits) && power >= float64(bounds[zone+1]) {
		zone++
	}
	return zone
}

// setPowerMetrics sets the power metrics of an activity, with the FTP of its user at its date: a ride with
// a power meter. The other activities get zeros (computed, nothing to count).
func setPowerMetrics(activity *models.Activity, ftp FTPFunc) error {
	activity.FTP, activity.NormalizedPower, activity.Intensity, activity.TSS = 0, nil, nil, nil
	activity.PowerZones, activity.PowerZoneBounds = make(types.FloatArray, models.PowerZones), nil
	if activity.Sport != models.FTPSport || len(activity.Powers) < 2 || ftp == nil || !hasMeasuredPower(*activity) {
		return nil
	}
	value, err := ftp(activity.Date, ownBest20(activity.BestEfforts))
	if err != nil || value == 0 {
		return err
	}
	times, powers := powerSamples(*activity)
	m := ComputePowerMetrics(times, powers, value)
	activity.FTP = value
	activity.PowerZones, activity.PowerZoneBounds = m.Zones, m.Bounds
	if m.NormalizedPower > 0 {
		activity.NormalizedPower, activity.Intensity, activity.TSS = &m.NormalizedPower, &m.Intensity, &m.TSS
	}
	return nil
}

// BackfillPowerMetrics computes the power metrics of the activities without them, oldest first (the FTP
// estimated at a date comes from the rides before): imported before they existed, re-analysed, or whose
// FTP changed. Their training score becomes their TSS, unless their device recorded one.
func BackfillPowerMetrics(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutPowerZones()
	if err != nil || len(ids) == 0 {
		return err
	}
	scored := 0
	for _, id := range ids {
		// stopping: what is done stays done, the rest goes on at the next start
		if ctx.Err() != nil {
			return nil
		}
		activity, err := db.GetActivityPower(id)
		if err != nil {
			return err
		}
		if err := setPowerMetrics(&activity, UserFTP(db, activity.UserID)); err != nil {
			return err
		}
		changed, err := rescore(ctx, db, &activity)
		if err != nil || ctx.Err() != nil {
			return err
		}
		if changed {
			scored++
		}
		if err := db.SetActivityPowerMetrics(&activity); err != nil {
			return err
		}
	}
	logger.Info().Int("activities", len(ids)).Int("scored", scored).Msg("power metrics computed")
	return nil
}

// rescore sets the training score of an activity whose power metrics changed: its TSS when it has one,
// unless its device recorded a score; its TRIMP again when its score was a TSS it hasn't anymore. True
// when the score changed.
func rescore(ctx context.Context, db *models.DB, activity *models.Activity) (bool, error) {
	source := activity.ScoreSource
	if source == "" && activity.TSS != nil {
		// scored before the sources were recorded: the device's score, or the TRIMP? Its file tells
		analysed, err := analyzeFileWhenFree(ctx, activity.FilePath, activity.User, activity.TrimEnd, Analysis{})
		if ctx.Err() != nil {
			return false, nil
		}
		if err == nil && analysed.ScoreSource == models.ScoreDevice {
			source = models.ScoreDevice
		}
		activity.ScoreSource = source
	}
	switch {
	case source == models.ScoreDevice:
		return false, nil
	case activity.TSS != nil:
		activity.Trimp, activity.ScoreSource = activity.TSS, models.ScorePower
		return true, nil
	case source == models.ScorePower:
		// no TSS anymore (the FTP is unknown): back to the heart rate
		activity.ScoreSource = models.ScoreHR
		return true, computeTrimp(activity, UserRestHR(db, activity.UserID))
	}
	return false, nil
}

// PowerRideChanged recomputes, in the background through run, the power metrics of the rides whose
// estimated FTP a ride added or deleted may change: the ones of the FTPWindowDays after it. added is false
// for a deletion. Nothing when the user set their FTP, or for an activity that isn't a ride with power.
func PowerRideChanged(db *models.DB, activity models.Activity, added bool, run func(func(ctx context.Context)),
	logger zerolog.Logger) {
	best := ownBest20(activity.BestEfforts)
	if activity.Sport != models.FTPSport || best <= 0 {
		return
	}
	if set, err := db.GetUserFTP(activity.UserID); err != nil || set > 0 {
		return
	}
	ftp := EstimateFTP(best)
	from, to := activity.Date.Add(time.Nanosecond), activity.Date.AddDate(0, 0, FTPWindowDays)
	var reset int64
	var err error
	if added {
		// their FTP may rise to the one of the new ride
		reset, err = db.ResetPowerMetricsBelow(activity.UserID, from, to, ftp)
	} else {
		// their FTP was the one of the deleted ride: it may fall
		reset, err = db.ResetPowerMetricsAt(activity.UserID, from, to, ftp)
	}
	if err != nil {
		logger.Error().Err(err).Str("activity", activity.ID.String()).Msg("power metrics not reset")
		return
	}
	if reset == 0 {
		return
	}
	run(func(ctx context.Context) {
		if err := BackfillPowerMetrics(ctx, db, logger); err != nil {
			logger.Error().Err(err).Uint("user_id", activity.UserID).Msg("power metrics not computed")
		}
	})
}

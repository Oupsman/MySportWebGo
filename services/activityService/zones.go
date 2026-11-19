package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"math"

	"github.com/rs/zerolog"
)

// HRZoneLimits are the lower limits of the zones 2 to 5, as fractions of the heart rate reserve (Karvonen):
// zone 1 holds everything below 60% (recovery), zone 5 everything from 90% (above the maximum too), so
// the zones add up to the whole time with heart rate
var HRZoneLimits = [models.HRZones - 1]float64{0.6, 0.7, 0.8, 0.9}

// TimeInZones returns the seconds an activity spent in each heart rate zone, and the heart rates (bpm)
// bounding them: the resting one, the lower limits of the zones 2 to 5, the maximum one. The samples are
// counted as in BanisterTRIMP: each interval at the heart rate of its end, the gaps longer than trimpMaxGap
// skipped. Zeros and no bounds without heart rate stream, or when the maximum heart rate of the user isn't
// above restHR.
func TimeInZones(activity models.Activity, restHR uint16) (types.FloatArray, types.Uint16Array) {
	zones := make(types.FloatArray, models.HRZones)
	maxHR := activity.User.GetMaxHR(activity.Date)
	// 0 without date of birth: the maximum can't be estimated
	if maxHR <= restHR || maxHR > 250 {
		return zones, nil
	}
	if len(activity.Hearts) < 2 || len(activity.HeartTimeStamps) != len(activity.Hearts) {
		return zones, nil
	}
	reserve := float64(maxHR - restHR)
	bounds := types.Uint16Array{restHR}
	for _, limit := range HRZoneLimits {
		bounds = append(bounds, restHR+uint16(math.Round(limit*reserve)))
	}
	bounds = append(bounds, maxHR)
	for i := 1; i < len(activity.Hearts); i++ {
		dt := activity.HeartTimeStamps[i] - activity.HeartTimeStamps[i-1]
		if dt <= 0 || dt > trimpMaxGap {
			continue
		}
		zones[hrZone(activity.Hearts[i], bounds)] += dt
	}
	for i := range zones {
		zones[i] = math.Round(zones[i]*10) / 10
	}
	return zones, bounds
}

// hrZone returns the index of the zone of a heart rate: compared to the bounds returned by TimeInZones, in
// bpm, so that a heart rate is in the zone the user is shown
func hrZone(hr uint16, bounds types.Uint16Array) int {
	zone := 0
	for zone < len(HRZoneLimits) && hr >= bounds[zone+1] {
		zone++
	}
	return zone
}

// setHRZones sets the time in heart rate zones of an activity, with the resting heart rate of its user at
// its date
func setHRZones(activity *models.Activity, restHR RestHRFunc) error {
	rest, err := restHRAt(restHR, activity.Date)
	if err != nil {
		return err
	}
	activity.HrZones, activity.HrZoneBounds = TimeInZones(*activity, rest)
	return nil
}

// BackfillHrZones sets the time in heart rate zones of the activities without it: imported before it
// existed, or whose user changed their maximum heart rate. Only the stored heart rate stream is used.
func BackfillHrZones(db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutHrZones()
	if err != nil || len(ids) == 0 {
		return err
	}
	for _, id := range ids {
		activity, err := db.GetActivityHearts(id)
		if err != nil {
			return err
		}
		if err := setHRZones(&activity, UserRestHR(db, activity.UserID)); err != nil {
			return err
		}
		if err := db.SetActivityHrZones(id, activity.HrZones, activity.HrZoneBounds); err != nil {
			return err
		}
	}
	logger.Info().Int("activities", len(ids)).Msg("heart rate zones computed")
	return nil
}

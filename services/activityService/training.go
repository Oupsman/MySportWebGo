package activityService

import (
	"MySportWeb/internal/pkg/models"
	// "MySportWeb/internal/pkg/utils" // used by the dead code below
	"math"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/rs/zerolog"
)

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func CalculateCalories(activity models.Activity) float64 {
// 	return (float64(activity.Duration) * 60 * GetMet(activity.Sport) * 3.5 * float64(activity.TotalWeight)) / 200
// }

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func GetMet(sport string) float64 {
// 	switch sport {
// 	case "cycling":
// 		return 9.5
// 	case "running":
// 		return 10
// 	case "hiking":
// 		return 6
// 	}
// 	return 6
// }

// DefaultRestHR is the resting heart rate (bpm) of the users who never measured theirs
const DefaultRestHR = 60

// trimpMaxGap is the longest time (seconds) between two heart rate samples that counts: a longer gap is a
// pause or a sensor dropout, not time spent at the heart rate of the sample
const trimpMaxGap = 30

// BanisterTRIMP returns the training impulse of an activity (Banister, 1991), integrated over its heart rate
// stream: each sample weighs its duration in minutes by x·a·e^(b·x), x being the fraction of the heart rate
// reserve, a=0.64 b=1.92 for men, a=0.86 b=1.67 for women. It's 0 without heart rate stream, or when the
// maximum heart rate of the user isn't above restHR.
func BanisterTRIMP(activity models.Activity, restHR uint16) float64 {
	maxHR := activity.User.GetMaxHR(activity.Date)
	// 0 without date of birth: the maximum can't be estimated
	if maxHR <= restHR || maxHR > 250 {
		return 0
	}
	if len(activity.Hearts) < 2 || len(activity.HeartTimeStamps) != len(activity.Hearts) {
		return 0
	}
	a, b := 0.64, 1.92
	if activity.User.Gender == 1 { // GetMaxHR adds 6 bpm for women
		a, b = 0.86, 1.67
	}
	reserve := float64(maxHR - restHR)
	var trimp float64
	for i := 1; i < len(activity.Hearts); i++ {
		dt := activity.HeartTimeStamps[i] - activity.HeartTimeStamps[i-1]
		if dt <= 0 || dt > trimpMaxGap {
			continue
		}
		x := (float64(activity.Hearts[i]) - float64(restHR)) / reserve
		x = math.Max(0, math.Min(1, x))
		trimp += dt / 60 * x * a * math.Exp(b*x)
	}
	return math.Round(trimp*10) / 10
}

// UserRestHR returns the resting heart rates of a user measured in the health data
func UserRestHR(db *models.DB, userID uint) RestHRFunc {
	return func(date time.Time) (uint16, error) {
		return db.GetRestHR(userID, date)
	}
}

// embeddedTrainingScore returns the training score the device recorded in the sessions: Garmin's training
// load, else the TSS. False when no session has one.
func embeddedTrainingScore(sessions []*mesgdef.Session) (float64, bool) {
	var score float64
	found := false
	for _, session := range sessions {
		value := valid(session.TrainingLoadPeakScaled())
		if value <= 0 {
			value = valid(session.TrainingStressScoreScaled())
		}
		if value > 0 {
			score += value
			found = true
		}
	}
	return math.Round(score*10) / 10, found
}

// setTrainingScore sets the training score of an analysed activity: the one recorded by the device, else
// its Banister TRIMP. A trimmed activity always gets its TRIMP: the device scored the whole recording.
func setTrainingScore(activity *models.Activity, sessions []*mesgdef.Session, trimmed bool, restHR RestHRFunc) error {
	if !trimmed {
		if score, ok := embeddedTrainingScore(sessions); ok {
			activity.Trimp = &score
			return nil
		}
	}
	return computeTrimp(activity, restHR)
}

// computeTrimp sets the Banister TRIMP of an activity, with the resting heart rate of its user at its date
func computeTrimp(activity *models.Activity, restHR RestHRFunc) error {
	rest, err := restHRAt(restHR, activity.Date)
	if err != nil {
		return err
	}
	trimp := BanisterTRIMP(*activity, rest)
	activity.Trimp = &trimp
	return nil
}

// restHRAt returns the resting heart rate of the user at date, DefaultRestHR when it's unknown
func restHRAt(restHR RestHRFunc, date time.Time) (uint16, error) {
	var rest uint16
	if restHR != nil {
		var err error
		if rest, err = restHR(date); err != nil {
			return 0, err
		}
	}
	if rest == 0 {
		rest = DefaultRestHR
	}
	return rest, nil
}

// BackfillTrimp sets the training score of the activities imported before it existed: their FIT file is
// analysed again for the score the device recorded; without readable file, the TRIMP is computed from the
// stored heart rate stream
func BackfillTrimp(db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutTrimp()
	if err != nil || len(ids) == 0 {
		return err
	}
	fromHearts := 0
	for _, id := range ids {
		activity, err := db.GetActivityHearts(id)
		if err != nil {
			return err
		}
		restHR := UserRestHR(db, activity.UserID)
		// only the heart rates are used: the altitudes needn't be corrected
		analysed, err := analyzeFile(activity.FilePath, activity.User, activity.TrimEnd, restHR, nil)
		if err == nil && analysed.Trimp != nil {
			activity.Trimp = analysed.Trimp
		} else {
			fromHearts++
			if err := computeTrimp(&activity, restHR); err != nil {
				return err
			}
		}
		if err := db.SetActivityTrimp(id, *activity.Trimp); err != nil {
			return err
		}
	}
	logger.Info().Int("activities", len(ids)).Int("without_file", fromHearts).Msg("training scores computed")
	return nil
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func CalculateTrainingEffect(activity models.Activity) float64 {
// 	powerNorm := utils.NormalizedAvg(activity.Powers)
// 	return powerNorm / activity.User.EstimateFTP(activity.Date)
// }

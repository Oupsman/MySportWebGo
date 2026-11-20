package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	// DefaultRiderWeight (kg) is the weight of the users who never told theirs: the default of their profile
	DefaultRiderWeight = 70.0
	// DefaultEquipmentWeight (kg) is the weight of a bike without one, for the rides: a road or city bike
	DefaultEquipmentWeight = 10.0
)

// WeightFunc returns the weight moved during an activity of a sport at a date (kg): the rider and their
// equipment
type WeightFunc func(date time.Time, sport string) (float64, error)

// estimatesPower tells whether the power of a sport is estimated from the speed and the weight without power
// meter: the rides, and the multisport activities, whose bike legs are
func estimatesPower(sport string) bool {
	switch sport {
	case "cycling", "triathlon", "duathlon", "multisport":
		return true
	}
	return false
}

// combineWeights returns the weight of a rider and their equipment, the defaults for the unknown ones: the
// equipment counts only for the sports whose power is estimated
func combineWeights(rider, equipment float64, sport string) float64 {
	if rider <= 0 {
		rider = DefaultRiderWeight
	}
	if equipment <= 0 && estimatesPower(sport) {
		equipment = DefaultEquipmentWeight
	}
	return rider + math.Max(equipment, 0)
}

// totalWeight returns the weight of an activity with weight, the defaults when nil
func totalWeight(weight WeightFunc, date time.Time, sport string) (float64, error) {
	if weight == nil {
		return combineWeights(0, 0, sport), nil
	}
	return weight(date, sport)
}

// UserWeight returns the weight of a user at the date of an activity (models.GetRiderWeight) with an
// equipment of equipmentWeight kg (0 for none)
func UserWeight(db *models.DB, userID uint, equipmentWeight int) WeightFunc {
	return func(date time.Time, sport string) (float64, error) {
		rider, err := db.GetRiderWeight(userID, date)
		if err != nil {
			return 0, err
		}
		return combineWeights(rider, float64(equipmentWeight), sport), nil
	}
}

// ActivityWeight returns the weight of a user at the date of an activity with its equipment (nil for none)
func ActivityWeight(db *models.DB, userID uint, equipmentID *uuid.UUID) WeightFunc {
	return func(date time.Time, sport string) (float64, error) {
		var equipment int
		if equipmentID != nil {
			var err error
			if equipment, err = db.GetEquipmentWeight(*equipmentID); err != nil {
				return 0, err
			}
		}
		return UserWeight(db, userID, equipment)(date, sport)
	}
}

// riderWeightAt returns the weight of a rider at a date as models.GetRiderWeight does, from their measures
// (oldest first) and the weight of their profile
func riderWeightAt(measures []models.WeightMeasure, profile float64, date time.Time) float64 {
	// the first measure after the date
	after := sort.Search(len(measures), func(i int) bool { return measures[i].Date.After(date) })
	switch {
	case after > 0:
		return measures[after-1].Weight
	case after < len(measures):
		return measures[after].Weight
	}
	return profile
}

// BackfillWeights analyses again the activities analysed with another weight than the one they have now:
// before the weights were used (110 kg for everyone), or after a weight measured since, a new weight of
// their equipment, another equipment. Only the rides have a power depending on it, the others only get the
// new weight. Stops at the next activity when ctx is done; what is done stays done.
func BackfillWeights(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	rows, err := db.GetActivityWeights()
	if err != nil {
		return err
	}
	profiles, err := db.GetProfileWeights()
	if err != nil {
		return err
	}
	measures := map[uint][]models.WeightMeasure{}
	var stale []models.ActivityWeightRow
	for _, row := range rows {
		m, found := measures[row.UserID]
		if !found {
			if m, err = db.GetWeightMeasures(row.UserID); err != nil {
				return err
			}
			measures[row.UserID] = m
		}
		rider := riderWeightAt(m, profiles[row.UserID], row.Date)
		weight := uint16(math.Round(combineWeights(rider, float64(row.EquipmentWeight), row.Sport)))
		if weight != row.TotalWeight {
			stale = append(stale, row)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	logger = logger.With().Str("backfill", "weights").Logger()
	progress := newBackfillProgress(logger, len(stale), "analysing again the activities with a new weight")
	failed := 0
	for i, row := range stale {
		if err := ctx.Err(); err != nil {
			logger.Info().Int("done", i).Int("total", len(stale)).Msg("weights backfill stopped, it goes on at the next start")
			return nil
		}
		if err := reweigh(ctx, db, row); err != nil {
			if !errors.Is(err, context.Canceled) {
				failed++
				logger.Warn().Err(err).Str("activity", row.ID.String()).Msg("activity not analysed again with its weight")
			}
		}
		progress.done(i+1, func(e *zerolog.Event) *zerolog.Event { return e.Int("failed", failed) })
	}
	logger.Info().Int("activities", len(stale)).Int("failed", failed).Msg("activities analysed again with their weight")
	return nil
}

// reweigh analyses an activity again with its weight and saves what depends on it, waiting for an analysis
// slot as long as the server is busy
func reweigh(ctx context.Context, db *models.DB, row models.ActivityWeightRow) error {
	stored, err := db.GetActivityToReanalyze(row.ID)
	if err != nil {
		return err
	}
	weight := ActivityWeight(db, stored.UserID, stored.EquipmentID)
	if !estimatesPower(row.Sport) {
		w, err := weight(row.Date, row.Sport)
		if err != nil {
			return err
		}
		return db.SetActivityTotalWeight(row.ID, uint16(math.Round(w)))
	}
	analysed, err := analyzeFileWhenFree(ctx, stored.FilePath, stored.User, stored.TrimEnd,
		Analysis{Correction: DeviceCorrection(db, stored.UserID), Weight: weight})
	if err != nil {
		return err
	}
	return db.UpdateActivityElevation(row.ID, &analysed)
}

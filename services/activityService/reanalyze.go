package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"fmt"

	"github.com/rs/zerolog"
)

// analyzeFile runs SumAnalyze, turning its panics (a FIT file without session for instance) into errors
func analyzeFile(filePath string) (activity models.Activity, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while analysing: %v", r)
		}
	}()
	return SumAnalyze(filePath, models.Users{}, models.Equipments{})
}

// BackfillTimeStamps re-reads the FIT file of the activities analysed by an older engine, to fill the
// time stamps of the sparse series. Activities whose file can't be read are marked as done without them,
// the frontend then falls back on the records time stamps.
func BackfillTimeStamps(db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesBelowEngine(vars.Engine_version)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	logger.Info().Int("activities", len(ids)).Int("engine_version", vars.Engine_version).Msg("re-analysing activities")
	failed := 0
	for _, id := range ids {
		filePath, err := db.GetActivityFilePath(id)
		if err != nil {
			return err
		}
		analysed, err := analyzeFile(filePath)
		if err != nil {
			failed++
			logger.Warn().Err(err).Str("activity", id.String()).Str("file", filePath).Msg("FIT file can't be re-analysed")
			err = db.UpdateActivityTimeStamps(id, nil, vars.Engine_version)
		} else {
			err = db.UpdateActivityTimeStamps(id, &analysed, vars.Engine_version)
		}
		if err != nil {
			return err
		}
	}
	logger.Info().Int("activities", len(ids)).Int("failed", failed).Msg("activities re-analysed")
	return nil
}

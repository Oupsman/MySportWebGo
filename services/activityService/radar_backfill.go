package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"time"

	"github.com/rs/zerolog"
)

// BackfillRadarEvents reads the FIT file of the activities whose radar events were never computed (the
// migration leaves only the ones of a Hammerhead device), as they were trimmed. An activity whose file
// can't be read gets none.
func BackfillRadarEvents(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutRadarEvents()
	if err != nil || len(ids) == 0 {
		return err
	}
	logger = logger.With().Str("backfill", "radar_events").Logger()
	progress := newBackfillProgress(ctx, logger, "activities", len(ids), "reading the radar events of activities")
	unreadable := 0
	for i, id := range ids {
		// stopping: what is done stays done, the rest goes on at the next start
		if ctx.Err() != nil {
			return nil
		}
		activity, err := db.GetActivityDistances(id)
		if err != nil {
			return err
		}
		// only the records are used: no resting heart rate nor altitude correction
		analysed, err := analyzeFileWhenFree(ctx, activity.FilePath, activity.User, activity.TrimEnd, Analysis{})
		if ctx.Err() != nil {
			// stopped while waiting for an analysis slot: the file wasn't read, not unreadable
			return nil
		}
		if err != nil {
			unreadable++
			logger.Warn().Err(err).Str("activity", id.String()).Msg("FIT file can't be read for its radar events")
		}
		if err := db.SetActivityRadarEvents(id, analysed.RadarEvents); err != nil {
			return err
		}
		progress.done(i+1, func(e *zerolog.Event) *zerolog.Event { return e.Int("unreadable", unreadable) })
	}
	logger.Info().Int("activities", len(ids)).Int("unreadable", unreadable).
		Str("elapsed", time.Since(progress.started).Round(time.Second).String()).Msg("radar events read")
	return nil
}

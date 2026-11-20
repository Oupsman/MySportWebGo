package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// BackfillThumbnails draws again the thumbnails drawn by an older version than ThumbnailVersion, the most
// recent activities first. Their tiles are downloaded MaxDownloads at a time and kept: the activities of the
// same places share them. When a tile can't be obtained (the tile server is down), it stops until the next
// start rather than keeping a thumbnail with holes; without tile server (MAP_TILE_URL=none), the
// thumbnails are drawn with the tiles there are.
func BackfillThumbnails(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetThumbnailsBelowVersion(ThumbnailVersion)
	if err != nil || len(ids) == 0 {
		return err
	}
	logger = logger.With().Str("backfill", "thumbnails").Logger()
	progress := newBackfillProgress(logger, len(ids), "drawing the thumbnails of activities again")
	for i, id := range ids {
		// stopping: what is done stays done, the rest goes on at the next start
		if ctx.Err() != nil {
			return nil
		}
		thumbnail, err := db.GetThumbnailToDraw(id)
		if err != nil {
			return err
		}
		// an activity without user has no media directory: nothing to draw
		if thumbnail.UserUUID != uuid.Nil {
			activity := models.Activity{ID: id, User: models.Users{UUID: thumbnail.UserUUID},
				PublicGpsPoints: thumbnail.PublicGpsPoints}
			missing, err := drawThumbnailWhenFree(ctx, activity)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			if missing > 0 && MapTiles != nil && MapTiles.Downloads() {
				logger.Warn().Int("missing_tiles", missing).Int("left", len(ids)-i).
					Msg("map tiles unavailable: the thumbnails are drawn again at the next start")
				return nil
			}
		}
		if err := db.SetThumbnailVersion(id, ThumbnailVersion); err != nil {
			return err
		}
		progress.done(i+1, nil)
	}
	logger.Info().Int("activities", len(ids)).
		Str("elapsed", time.Since(progress.started).Round(time.Second).String()).Msg("thumbnails drawn again")
	return nil
}

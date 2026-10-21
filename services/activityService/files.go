package activityService

import (
	"MySportWeb/internal/pkg/models"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// MediaDir is the directory of the users' files, relative to the working directory
const MediaDir = "MEDIA"

// ThumbnailPath returns the path of the thumbnail of an activity
func ThumbnailPath(userUUID, activityID uuid.UUID) string {
	return filepath.Join(MediaDir, userUUID.String(), "thumbnails", activityID.String()+".png")
}

// removeUserFile removes a file of a user, refusing any path outside of the user's media directory
func removeUserFile(userUUID uuid.UUID, path string) error {
	userDir := filepath.Join(MediaDir, userUUID.String()) + string(filepath.Separator)
	if !strings.HasPrefix(filepath.Clean(path), userDir) {
		return errors.New("refusing to remove " + path + ": not in " + userDir)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// DeleteActivity deletes an activity, then its thumbnail and its FIT file. The FIT file is kept while
// other activities use it: uploading a file again creates a new activity with the same path.
// Files that can't be removed are logged: the activity is gone, they only waste space.
func DeleteActivity(db *models.DB, activity *models.Activity, logger zerolog.Logger) error {
	if err := db.DeleteActivity(activity.ID); err != nil {
		return err
	}
	userUUID := activity.User.UUID
	if err := removeUserFile(userUUID, ThumbnailPath(userUUID, activity.ID)); err != nil {
		logger.Warn().Err(err).Str("activity", activity.ID.String()).Msg("thumbnail not removed")
	}
	if activity.FilePath == "" {
		return nil
	}
	others, err := db.CountActivitiesWithFile(activity.FilePath)
	if err != nil {
		logger.Warn().Err(err).Str("activity", activity.ID.String()).Msg("FIT file not removed")
		return nil
	}
	if others == 0 {
		if err := removeUserFile(userUUID, activity.FilePath); err != nil {
			logger.Warn().Err(err).Str("activity", activity.ID.String()).Msg("FIT file not removed")
		}
	}
	return nil
}

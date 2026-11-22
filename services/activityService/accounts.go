package activityService

import (
	"MySportWeb/internal/pkg/models"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// UserMediaDir is the directory of the files of a user: FIT, GPX and TCX files, thumbnails, pictures
func UserMediaDir(userUUID uuid.UUID) string {
	return filepath.Join(MediaDir, userUUID.String())
}

// DeleteUser deletes a user, everything they own (models.DB.DeleteUser), then their files. Files that can't
// be removed are logged: the user is gone, cleanmedia finds them. models.ErrLastAdmin for the last admin.
func DeleteUser(db *models.DB, id uint, logger zerolog.Logger) (models.Users, error) {
	user, err := db.DeleteUser(id)
	if err != nil {
		return user, err
	}
	if user.UUID != uuid.Nil {
		if err := os.RemoveAll(UserMediaDir(user.UUID)); err != nil {
			logger.Warn().Err(err).Uint("user_id", id).Msg("files of the deleted user not removed")
		}
	}
	logger.Info().Uint("user_id", id).Str("username", user.Username).Msg("user deleted")
	return user, nil
}

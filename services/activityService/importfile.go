package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ImportOptions are the choices of ImportFile
type ImportOptions struct {
	// Sport is the sport of a GPX or TCX file that doesn't name one (a FIT sport name, "" for generic)
	Sport string
	// Wait waits for an analysis and a thumbnail slot as long as the server is busy, until ctx is done: the
	// background jobs. Without it, ErrBusy after QueueTimeout: the uploads, the client sends them again.
	Wait bool
	// ResetLastImport clears the "last import" mark of the other activities of the user: the first file of
	// a batch of uploads
	ResetLastImport bool
	// Logger logs what fails after the activity is saved; the zero Logger logs nothing
	Logger zerolog.Logger
}

// ActivitiesDir is the directory of the FIT files of a user, and of the GPX and TCX files they come from
func ActivitiesDir(userUUID uuid.UUID) string {
	return filepath.Join(UserMediaDir(userUUID), "Activities")
}

// CreateImportFile creates an empty temporary file in the activities directory of a user, for ImportFile:
// it is moved in place from there, on the same file system. The caller removes it once ImportFile returns
// (harmless when it was moved).
func CreateImportFile(userUUID uuid.UUID) (string, error) {
	dir := ActivitiesDir(userUUID)
	if err := os.MkdirAll(dir, 0770); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "upload-*.fit")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// ImportFile creates the activity of user from the file at path, a FIT, GPX or TCX file, gzipped or not,
// uploaded under the name name: analysed, given the default equipment of its sport, its thumbnail drawn, its
// files stored, linked to its route and its device, its passes on the segments of the user found. path is a
// file of CreateImportFile; it becomes the FIT file of the activity. A GPX or TCX file is converted, and kept
// beside it.
//
// Errors: *InvalidFitError (and ErrNoFitFile) for a file that isn't an activity, *DuplicateError when the
// user already has the activity, ErrBusy without opts.Wait, ctx.Err() with it.
//
// The follow-ups that need the application are the caller's: PowerRideChanged, the weather, the
// notification of the new bests.
func ImportFile(ctx context.Context, db *models.DB, user models.Users, path, name string,
	opts ImportOptions) (models.Activity, error) {
	prepare, analyze, draw := PrepareUpload, analyzeFile, drawThumbnail
	if opts.Wait {
		prepare = func(path, name, sport string) (PreparedUpload, error) {
			return prepareUploadWhenFree(ctx, path, name, sport)
		}
		analyze = func(path string, user models.Users, trimEnd float64, analysis Analysis) (models.Activity, error) {
			return analyzeFileWhenFree(ctx, path, user, trimEnd, analysis)
		}
		draw = func(activity models.Activity) (int, error) { return drawThumbnailWhenFree(ctx, activity) }
	}

	// a GPX or TCX file becomes a FIT file, a gzipped one is decompressed
	prepared, err := prepare(path, name, opts.Sport)
	if err != nil {
		return models.Activity{}, err
	}
	if prepared.SourcePath != "" {
		defer os.Remove(prepared.SourcePath) // fails harmlessly once the file is moved in place
	}
	activity, err := analyze(path, user, 0, Analysis{
		RestHR:     UserRestHR(db, user.ID),
		Correction: DeviceCorrection(db, user.ID),
		Weight:     ImportWeight(db, user.ID),
		FTP:        UserFTP(db, user.ID),
	})
	if err != nil {
		return models.Activity{}, err
	}
	activity.UserID = user.ID
	if err := AssignDefaultEquipment(db, &activity); err != nil {
		return models.Activity{}, err
	}
	activity.ID = uuid.New()
	activity.Filename = filepath.Base(name)

	// checked again when saving; checked here to answer before drawing the thumbnail, which is slow
	existing, err := db.FindDuplicateActivity(user.ID, activity.Sport, activity.Date)
	if err != nil {
		return models.Activity{}, err
	}
	if existing != nil {
		return models.Activity{}, &DuplicateError{ExistingID: *existing}
	}

	if opts.ResetLastImport {
		if err := db.ResetImportStatus(user.ID); err != nil {
			return models.Activity{}, err
		}
	}

	if _, err := draw(activity); err != nil {
		return models.Activity{}, err
	}
	activity.ThumbnailVersion = ThumbnailVersion

	// the FIT file keeps its name, prefixed by the activity id when another file has it; the GPX or TCX
	// file it was converted from is kept beside it, never served
	dir := filepath.Dir(path)
	store := func(activity *models.Activity) error {
		prefix := ""
		for _, name := range []string{prepared.FITName, prepared.SourceName} {
			if _, err := os.Stat(filepath.Join(dir, name)); name != "" && err == nil {
				prefix = activity.ID.String() + "_"
			}
		}
		activity.FilePath = filepath.Join(dir, prefix+prepared.FITName)
		if prepared.SourcePath != "" {
			activity.SourceFilePath = filepath.Join(dir, prefix+prepared.SourceName)
			if err := os.Rename(prepared.SourcePath, activity.SourceFilePath); err != nil {
				return err
			}
		}
		if err := os.Rename(path, activity.FilePath); err != nil {
			if activity.SourceFilePath != "" {
				os.Remove(activity.SourceFilePath)
			}
			return err
		}
		return nil
	}
	if err := CreateActivity(db, &activity, store); err != nil {
		os.Remove(ThumbnailPath(user.UUID, activity.ID))
		return models.Activity{}, err
	}
	// its passes on the segments of the user: the activity is saved, a failure only misses them
	if err := MatchActivitySegments(db, activity.ID, MatchUpload); err != nil {
		opts.Logger.Error().Err(err).Str("activity", activity.ID.String()).Msg("passes on the segments not found")
	}
	return activity, nil
}

// prepareUploadWhenFree runs PrepareUpload, waiting for an analysis slot as long as the server is busy
func prepareUploadWhenFree(ctx context.Context, path, name, sport string) (PreparedUpload, error) {
	for {
		prepared, err := PrepareUpload(path, name, sport)
		if !errors.Is(err, ErrBusy) {
			return prepared, err
		}
		if err := waitRetry(ctx); err != nil {
			return PreparedUpload{}, err
		}
	}
}

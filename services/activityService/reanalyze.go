package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/vars"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// DefaultSecurityDistance is the radius (in meters) of the privacy zones of a user, the default of the users
// table: the one of the activities whose user can't be read
const DefaultSecurityDistance = 500

// analyzeFile runs AnalyzeTrimmed, turning its panics (a FIT file without session for instance) into errors
func analyzeFile(filePath string, user models.Users, trimEnd float64, opts Analysis) (activity models.Activity, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while analysing: %v", r)
		}
	}()
	return AnalyzeTrimmed(filePath, user, models.Equipments{}, trimEnd, opts)
}

// analyzeFileWhenFree runs analyzeFile, waiting for an analysis slot as long as the server is busy: a
// background job must not take a refused analysis for an unreadable file. ctx.Err() when ctx is done first.
func analyzeFileWhenFree(ctx context.Context, filePath string, user models.Users, trimEnd float64, opts Analysis) (models.Activity, error) {
	for {
		activity, err := analyzeFile(filePath, user, trimEnd, opts)
		if !errors.Is(err, ErrBusy) {
			return activity, err
		}
		if err := waitRetry(ctx); err != nil {
			return models.Activity{}, err
		}
	}
}

// drawThumbnailWhenFree runs drawThumbnail, waiting for a drawing slot as long as the server is busy
func drawThumbnailWhenFree(ctx context.Context, activity models.Activity) (missing int, err error) {
	for {
		missing, err := drawThumbnail(activity)
		if !errors.Is(err, ErrBusy) {
			return missing, err
		}
		if err := waitRetry(ctx); err != nil {
			return 0, err
		}
	}
}

// BackfillTimeStamps re-reads the FIT file of the activities analysed by an older engine, to fill the
// time stamps of the sparse series, the recording device, the power, the temperatures, and the privacy
// zones of the public trace, with the security distance of the user. The thumbnails, drawn from the public
// trace, are drawn again when it changed.
// Activities whose file can't be read are marked as done without them, the frontend then falls back on the
// records time stamps; their public trace is computed again from the trace saved in the database.
func BackfillTimeStamps(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
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
		// stopping: what is done stays done, the rest goes on at the next start
		if ctx.Err() != nil {
			return nil
		}
		stored, err := db.GetActivityToReanalyze(id)
		if err != nil {
			return err
		}
		user := stored.User
		if user.ID == 0 {
			// a zone of 0 would make the whole trace public
			user.SecurityDistance = DefaultSecurityDistance
		}
		// the series must stay as long as the trimmed activity
		analysed, err := analyzeFileWhenFree(ctx, stored.FilePath, user, stored.TrimEnd,
			Analysis{Correction: DeviceCorrection(db, stored.UserID), Weight: ActivityWeight(db, stored.UserID, stored.EquipmentID)})
		if ctx.Err() != nil {
			// stopped while waiting for an analysis slot: the file wasn't read, not unreadable
			return nil
		}
		var public types.GpsPointArray
		if err != nil {
			failed++
			logger.Warn().Err(err).Str("activity", id.String()).Str("file", stored.FilePath).Msg("FIT file can't be re-analysed")
			public = publicTraceOf(stored.GpsPoints, user.SecurityDistance)
			err = db.UpdateActivityTimeStamps(id, nil, &public, vars.Engine_version)
		} else {
			public = analysed.PublicGpsPoints
			// the new engine may name the device differently: the activity goes to the device of that name
			analysed.DeviceID, err = db.FindOrCreateDevice(stored.UserID, analysed.Device, analysed.DeviceName,
				models.AltitudeCorrectionNone)
			if err != nil {
				return err
			}
			err = db.UpdateActivityTimeStamps(id, &analysed, nil, vars.Engine_version)
			if err == nil {
				err = fixRouteEndpoints(db, stored.RouteID, analysed.StartPosition, analysed.EndPosition)
			}
		}
		if err != nil {
			return err
		}
		if user.UUID != uuid.Nil && !slices.Equal(public, stored.PublicGpsPoints) {
			redrawn := models.Activity{ID: id, User: user, PublicGpsPoints: public}
			if _, err := drawThumbnailWhenFree(ctx, redrawn); err != nil {
				logger.Warn().Err(err).Str("activity", id.String()).Msg("thumbnail not drawn again with the new public trace")
			}
		}
	}
	logger.Info().Int("activities", len(ids)).Int("failed", failed).Msg("activities re-analysed")
	return nil
}

// publicTraceOf computes the public trace from a saved trace, whose first and last points are the first and
// the last GPS fixes
func publicTraceOf(points types.GpsPointArray, securityDistance uint16) types.GpsPointArray {
	if len(points) == 0 {
		return nil
	}
	return PublicTrace(points, points[0], points[len(points)-1], securityDistance)
}

// fixRouteEndpoints gives a route the endpoints of one of its activities when its own are invalid: routes
// created by the engines before 5 from an activity whose first or last record had no GPS fix
func fixRouteEndpoints(db *models.DB, routeID *uuid.UUID, start, end *types.GpsPoint) error {
	if routeID == nil || !ValidPosition(start) || !ValidPosition(end) {
		return nil
	}
	route, err := db.GetRoute(*routeID)
	if err != nil {
		return err
	}
	if ValidPosition(route.StartPosition) && ValidPosition(route.EndPosition) {
		return nil
	}
	return db.SetRouteEndpoints(route.ID, start, end)
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Background runs work started by a request after its answer, until the server stops: app.App, which waits
// for it at the shutdown
type Background interface {
	Go(fn func(ctx context.Context))
}

// waitRetry waits BusyRetryDelay before trying again an analysis refused with ErrBusy, ctx.Err() when ctx is
// done first
func waitRetry(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(BusyRetryDelay):
		return nil
	}
}

// ErrRecomputeRunning is a recompute asked for a device whose activities are being recomputed
var ErrRecomputeRunning = errors.New("the activities of this device are already being recomputed")

// BusyRetryDelay is how long a recompute waits before trying again an analysis refused with ErrBusy
var BusyRetryDelay = 5 * time.Second

// the recomputes since the server started, by device. One server runs the jobs: a restart forgets them,
// the activities already done stay done and the user starts it again.
var recomputes = struct {
	sync.Mutex
	jobs map[uuid.UUID]*types.RecomputeStatus
}{jobs: map[uuid.UUID]*types.RecomputeStatus{}}

// RecomputeStatusOf returns the latest recompute of a device since the server started, nil without
func RecomputeStatusOf(deviceID uuid.UUID) *types.RecomputeStatus {
	recomputes.Lock()
	defer recomputes.Unlock()
	status, found := recomputes.jobs[deviceID]
	if !found {
		return nil
	}
	copied := *status
	return &copied
}

// StartRecompute analyses again, in the background, the activities of a device of a user with its altitude
// correction, and saves what depends on their altitudes (see models.UpdateActivityElevation): one activity
// at a time, the uploads keep the other analysis slots. ErrRecomputeRunning when a recompute of the device
// is running.
func StartRecompute(background Background, db *models.DB, userID uint, deviceID uuid.UUID, logger zerolog.Logger) (types.RecomputeStatus, error) {
	recomputes.Lock()
	defer recomputes.Unlock()
	if running, found := recomputes.jobs[deviceID]; found && running.Running {
		return *running, ErrRecomputeRunning
	}
	ids, err := db.GetDeviceActivities(userID, deviceID)
	if err != nil {
		return types.RecomputeStatus{}, err
	}
	status := &types.RecomputeStatus{Running: true, Total: len(ids), StartedAt: time.Now()}
	recomputes.jobs[deviceID] = status
	logger = logger.With().Str("device", deviceID.String()).Logger()
	background.Go(func(ctx context.Context) { recompute(ctx, db, userID, ids, status, logger) })
	return *status, nil
}

// recompute stops at the next activity when ctx is done: the status tells how many were done
func recompute(ctx context.Context, db *models.DB, userID uint, ids []uuid.UUID, status *types.RecomputeStatus, logger zerolog.Logger) {
	logger.Info().Int("activities", len(ids)).Msg("recomputing the altitudes of the activities of a device")
	correction := DeviceCorrection(db, userID)
	for _, id := range ids {
		if ctx.Err() != nil {
			logger.Info().Int("done", status.Done).Int("total", status.Total).Msg("recompute stopped by the shutdown")
			break
		}
		err := recomputeActivity(ctx, db, id, correction)
		recomputes.Lock()
		if err != nil {
			status.Failed++
		} else {
			status.Done++
		}
		recomputes.Unlock()
		if err != nil {
			logger.Warn().Err(err).Str("activity", id.String()).Msg("activity not recomputed")
		}
	}
	recomputes.Lock()
	finished := time.Now()
	status.Running = false
	status.FinishedAt = &finished
	recomputes.Unlock()
	logger.Info().Int("done", status.Done).Int("failed", status.Failed).Msg("altitudes recomputed")
}

// recomputeActivity analyses an activity again, as it was trimmed, waiting for an analysis slot as long as
// the server is busy
func recomputeActivity(ctx context.Context, db *models.DB, id uuid.UUID, correction CorrectionFunc) error {
	stored, err := db.GetActivityToReanalyze(id)
	if err != nil {
		return err
	}
	for {
		analysed, err := analyzeFile(stored.FilePath, stored.User, stored.TrimEnd,
			Analysis{Correction: correction, Weight: ActivityWeight(db, stored.UserID, stored.EquipmentID)})
		if errors.Is(err, ErrBusy) {
			if err := waitRetry(ctx); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		return db.UpdateActivityElevation(id, &analysed)
	}
}

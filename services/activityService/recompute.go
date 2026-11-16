package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

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
func StartRecompute(db *models.DB, userID uint, deviceID uuid.UUID, logger zerolog.Logger) (types.RecomputeStatus, error) {
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
	go recompute(db, userID, ids, status, logger)
	return *status, nil
}

func recompute(db *models.DB, userID uint, ids []uuid.UUID, status *types.RecomputeStatus, logger zerolog.Logger) {
	logger.Info().Int("activities", len(ids)).Msg("recomputing the altitudes of the activities of a device")
	correction := DeviceCorrection(db, userID)
	for _, id := range ids {
		err := recomputeActivity(db, id, correction)
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
func recomputeActivity(db *models.DB, id uuid.UUID, correction CorrectionFunc) error {
	stored, err := db.GetActivityToReanalyze(id)
	if err != nil {
		return err
	}
	for {
		analysed, err := analyzeFile(stored.FilePath, stored.User, stored.TrimEnd, nil, correction)
		if errors.Is(err, ErrBusy) {
			time.Sleep(BusyRetryDelay)
			continue
		}
		if err != nil {
			return err
		}
		return db.UpdateActivityElevation(id, &analysed)
	}
}

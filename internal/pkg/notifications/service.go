package notifications

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// ServiceCheckInterval is how often the equipments are checked for a service due
const ServiceCheckInterval = 15 * time.Minute

// RunServiceAlerts queues a "service due" notification for each equipment past its maintenance interval,
// once per service cycle (a new service starts the next one), every ServiceCheckInterval until ctx is done
func RunServiceAlerts(ctx context.Context, logger zerolog.Logger, db models.DB) {
	for {
		if err := CheckServices(db, logger); err != nil {
			logger.Error().Err(err).Msg("failed to check the services due")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(ServiceCheckInterval):
		}
	}
}

// CheckServices queues the notifications of the services due, see RunServiceAlerts
func CheckServices(db models.DB, logger zerolog.Logger) error {
	due, err := db.ServiceAlertsDue()
	if err != nil {
		return err
	}
	for _, equipment := range due {
		message := PushMessage{
			Title: "Service due: " + equipment.Name,
			Body:  serviceDueText(equipment),
			// its services, where the new one is added
			URL: "/#/equipment/" + equipment.ID.String() + "/edit#services",
		}
		queued, err := db.NotifyUser(equipment.UserID, message.Encode())
		if err != nil {
			return err
		}
		// the user unsubscribed meanwhile: notified when subscribing again
		if queued == 0 {
			continue
		}
		if err := db.SetServiceAlerted(equipment.ID, equipment.LastService); err != nil {
			return err
		}
		logger.Info().Str("equipment", equipment.ID.String()).Int("km_since_service", equipment.KmSinceService).
			Msg("service due: notified")
	}
	return nil
}

func serviceDueText(e models.ServiceDue) string {
	since := "since its first ride"
	if e.LastService != nil {
		since = "since its last service"
	}
	return fmt.Sprintf("%d km %s (a service every %d km).", e.KmSinceService, since, e.Interval)
}

package notifications

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ServiceCheckInterval is how often the maintenance reminders are checked
const ServiceCheckInterval = 15 * time.Minute

// RunServiceAlerts queues the notifications of the maintenance reminders due, once per cycle (a service done
// for a reminder starts its next one), of the goals reached (CheckGoals) and of the cheers given
// (CheckCheers), every ServiceCheckInterval until ctx is done
func RunServiceAlerts(ctx context.Context, logger zerolog.Logger, db models.DB) {
	for {
		if err := CheckServices(db, logger, time.Now()); err != nil {
			logger.Error().Err(err).Msg("failed to check the maintenance reminders")
		}
		if err := CheckGoals(db, logger, time.Now()); err != nil {
			logger.Error().Err(err).Msg("failed to check the goals")
		}
		if err := CheckCheers(db, logger, time.Now()); err != nil {
			logger.Error().Err(err).Msg("failed to notify the cheers")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(ServiceCheckInterval):
		}
	}
}

// CheckServices queues the notifications of the reminders due at now, one per equipment with all its
// reminders due, see RunServiceAlerts
func CheckServices(db models.DB, logger zerolog.Logger, now time.Time) error {
	due, err := db.RemindersToNotify(now)
	if err != nil {
		return err
	}
	var order []uuid.UUID
	byEquipment := map[uuid.UUID][]models.ReminderStatus{}
	for _, r := range due {
		if _, ok := byEquipment[r.EquipmentID]; !ok {
			order = append(order, r.EquipmentID)
		}
		byEquipment[r.EquipmentID] = append(byEquipment[r.EquipmentID], r)
	}
	for _, id := range order {
		reminders := byEquipment[id]
		message := reminderMessage(reminders)
		queued, err := db.NotifyUser(reminders[0].UserID, models.NotifyMaintenance, message.Encode())
		if err != nil {
			return err
		}
		// the user unsubscribed meanwhile: notified when subscribing again
		if queued == 0 {
			continue
		}
		for _, r := range reminders {
			if err := db.SetReminderNotified(r.ID, r.LastDone); err != nil {
				return err
			}
		}
		logger.Info().Str("equipment", id.String()).Int("reminders", len(reminders)).Msg("maintenance due: notified")
	}
	return nil
}

// reminderMessage is the notification of the reminders due of an equipment
func reminderMessage(reminders []models.ReminderStatus) PushMessage {
	lines := make([]string, len(reminders))
	for i, r := range reminders {
		lines[i] = r.Name + ": " + reminderReason(r)
	}
	title := "Maintenance due: " + reminders[0].EquipmentName
	if len(reminders) == 1 {
		title = reminders[0].Name + ": " + reminders[0].EquipmentName
	}
	return PushMessage{
		Title: title,
		Body:  strings.Join(lines, "\n"),
		// its reminders, where they are marked done
		URL: "/#/equipment/" + reminders[0].EquipmentID.String() + "/edit#reminders",
	}
}

// reminderReason tells why a reminder is due: the interval reached
func reminderReason(r models.ReminderStatus) string {
	since := "since it was last done"
	if r.LastDone == nil {
		since = "so far"
	}
	switch {
	case r.KmLeft != nil && *r.KmLeft <= 0:
		return fmt.Sprintf("%d km %s (every %d km).", r.KmSince, since, r.EveryKm)
	case r.HoursLeft != nil && *r.HoursLeft <= 0:
		return fmt.Sprintf("%s h of activities %s (every %d h).", formatHours(r.HoursSince), since, r.EveryHours)
	default:
		return "due " + dueDay(r) + " (" + Every(r.EveryCount, r.EveryUnit) + ")."
	}
}

func formatHours(h float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", h), ".0")
}

// dueDay is when a reminder due by its time interval became due
func dueDay(r models.ReminderStatus) string {
	if r.DueDate == nil {
		return "now"
	}
	date, err := time.Parse(time.DateOnly, *r.DueDate)
	if err != nil {
		return *r.DueDate
	}
	return "since " + date.Format("January 2")
}

// Every is a time interval in words: "every week", "every 3 months"
func Every(count int, unit string) string {
	if count == 1 {
		return "every " + unit
	}
	return fmt.Sprintf("every %d %ss", count, unit)
}

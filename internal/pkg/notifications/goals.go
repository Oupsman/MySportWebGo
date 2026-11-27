package notifications

import (
	"MySportWeb/internal/pkg/models"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// CheckGoals queues the notifications of the goals reached at now, once per period of a goal, checked with
// the maintenance reminders (RunServiceAlerts)
func CheckGoals(db models.DB, logger zerolog.Logger, now time.Time) error {
	reached, err := db.GoalsToNotify(now)
	if err != nil {
		return err
	}
	for _, goal := range reached {
		queued, err := db.NotifyUser(goal.UserID, models.NotifyGoal, goalMessage(goal).Encode())
		if err != nil {
			return err
		}
		// the user unsubscribed meanwhile: notified when subscribing again
		if queued == 0 {
			continue
		}
		if err := db.SetGoalNotified(goal.ID, goal.Current.Start); err != nil {
			return err
		}
		logger.Info().Str("goal", goal.ID.String()).Msg("goal reached: notified")
	}
	return nil
}

// goalMessage is the notification of a goal reached
func goalMessage(goal models.GoalStatus) PushMessage {
	title := "Goal reached"
	if goal.Name != "" {
		title += ": " + goal.Name
	}
	return PushMessage{
		Title: title,
		Body: fmt.Sprintf("%s: %s, for a target of %s.", DescribeGoal(goal),
			FormatGoalValue(goal.Metric, goal.Current.Value), FormatGoalValue(goal.Metric, goal.Target)),
		URL: "/#/goals",
	}
}

// DescribeGoal tells what a goal counts and over which period, e.g. "Cycling and running this week"
func DescribeGoal(goal models.GoalStatus) string {
	what := "All sports"
	if len(goal.Sports) > 0 {
		names := make([]string, len(goal.Sports))
		for i, sport := range goal.Sports {
			names[i] = strings.ReplaceAll(sport, "_", " ")
		}
		what = strings.Join(names, ", ")
		if len(names) > 1 {
			what = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
		}
		what = strings.ToUpper(what[:1]) + what[1:]
	}
	switch goal.Period {
	case "custom":
		return fmt.Sprintf("%s from %s to %s", what, goal.Current.Start, goal.Current.End)
	default:
		return what + " this " + goal.Period
	}
}

// FormatGoalValue formats a value of a metric: km, hours and minutes, m, activities
func FormatGoalValue(metric string, value float64) string {
	switch metric {
	case "distance":
		return fmt.Sprintf("%.1f km", value/1000)
	case "duration":
		minutes := int(value / 60)
		return fmt.Sprintf("%d h %02d", minutes/60, minutes%60)
	case "elevation":
		return fmt.Sprintf("%d m", int(value))
	default:
		if value == 1 {
			return "1 activity"
		}
		return fmt.Sprintf("%d activities", int(value))
	}
}

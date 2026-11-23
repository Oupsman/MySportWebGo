package notifications

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// HighlightsRecentDays: the new bests of an activity older than that aren't notified (an import of past
// activities); the goals have their own notification (CheckGoals)
const HighlightsRecentDays = 7

// highlightsShown is how many new bests a notification names
const highlightsShown = 3

// distanceNames are the running distances with a name, the others are in km (as the frontend)
var distanceNames = map[float64]string{400: "400 m", 1609.344: "1 mile", 21097.5: "half marathon", 42195: "marathon"}

// effortTarget names the target of an effort: "5 km", "marathon", "20 min", "5 s"
func effortTarget(kind string, target float64) string {
	if kind == types.EffortDistance {
		if name, ok := distanceNames[target]; ok {
			return name
		}
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.3f", target/1000), "0"), ".") + " km"
	}
	switch {
	case target < 60:
		return fmt.Sprintf("%.0f s", target)
	case target < 3600:
		return fmt.Sprintf("%.0f min", target/60)
	default:
		return fmt.Sprintf("%.0f h", target/3600)
	}
}

// formatTime writes seconds as h:mm:ss, m:ss under an hour
func formatTime(seconds float64) string {
	whole := int(seconds + 0.5)
	if whole >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", whole/3600, whole%3600/60, whole%60)
	}
	return fmt.Sprintf("%d:%02d", whole/60, whole%60)
}

// describeHighlight tells a new best: "5 km in 23:20 (−0:50)", "20 min at 260 W (+10 W)", "the hill in 4:50"
func describeHighlight(h types.Highlight) string {
	switch {
	case h.Kind == types.HighlightSegment:
		text := fmt.Sprintf("%s in %s", h.Name, formatTime(h.Value))
		if h.Previous != nil {
			text += fmt.Sprintf(" (−%s)", formatTime(*h.Previous-h.Value))
		}
		return text
	case h.EffortKind == types.EffortPower:
		text := fmt.Sprintf("%s at %.0f W", effortTarget(h.EffortKind, h.Target), h.Value)
		if h.Previous != nil {
			text += fmt.Sprintf(" (+%.0f W)", h.Value-*h.Previous)
		}
		return text
	default:
		text := fmt.Sprintf("%s in %s", effortTarget(h.EffortKind, h.Target), formatTime(h.Value))
		if h.Previous != nil {
			text += fmt.Sprintf(" (−%s)", formatTime(*h.Previous-h.Value))
		}
		return text
	}
}

// highlightsMessage is the notification of the new bests of an activity that still stand, false without
func highlightsMessage(activityID uuid.UUID, title string, highlights []types.Highlight) (PushMessage, bool) {
	var bests []string
	for _, h := range highlights {
		if h.Kind != types.HighlightGoal && h.Current {
			bests = append(bests, describeHighlight(h))
		}
	}
	if len(bests) == 0 {
		return PushMessage{}, false
	}
	body := strings.Join(bests[:min(len(bests), highlightsShown)], "; ")
	if len(bests) > highlightsShown {
		body += fmt.Sprintf(", and %d more", len(bests)-highlightsShown)
	}
	heading := "New best"
	if len(bests) > 1 {
		heading = "New bests"
	}
	return PushMessage{Title: fmt.Sprintf("%s: %s", heading, title), Body: body + ".",
		URL: "/#/activity/" + activityID.String()}, true
}

// NotifyHighlights queues the notification of the new bests of an activity just uploaded, still the bests:
// for an activity of the last HighlightsRecentDays only
func NotifyHighlights(db *models.DB, logger zerolog.Logger, userID uint, activityID uuid.UUID, title string,
	date time.Time, loc *time.Location, now time.Time) error {
	if now.Sub(date) > HighlightsRecentDays*24*time.Hour {
		return nil
	}
	highlights, err := db.GetHighlights(userID, []uuid.UUID{activityID}, loc)
	if err != nil {
		return err
	}
	message, ok := highlightsMessage(activityID, title, highlights)
	if !ok {
		return nil
	}
	if _, err := db.NotifyUser(userID, message.Encode()); err != nil {
		return err
	}
	logger.Info().Str("activity", activityID.String()).Msg("new bests: notified")
	return nil
}

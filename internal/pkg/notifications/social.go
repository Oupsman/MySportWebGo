package notifications

import (
	"MySportWeb/internal/pkg/models"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// CheerSettle is how long a cheer waits before its notification: one taken back meanwhile isn't notified,
// and the cheers given together make one notification
const CheerSettle = 5 * time.Minute

// cheerersShown is how many of the users who cheered a notification names
const cheerersShown = 2

// commentShown is how many characters of a comment its notification shows
const commentShown = 140

// activityURL is the page of an activity in the web application
func activityURL(id uuid.UUID) string {
	return "/#/activity/" + id.String()
}

// cheersMessage is the notification of the cheers of an activity: "alice and bob cheered", "alice, bob and 3
// others cheered"
func cheersMessage(batch models.CheerBatch) PushMessage {
	names := batch.Usernames
	var who string
	switch {
	case len(names) == 1:
		who = names[0]
	case len(names) <= cheerersShown+1:
		who = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	default:
		others := len(names) - cheerersShown
		who = fmt.Sprintf("%s and %d others", strings.Join(names[:cheerersShown], ", "), others)
	}
	return PushMessage{Title: who + " cheered your activity", Body: batch.Title, URL: activityURL(batch.ActivityID)}
}

// CheckCheers queues the notifications of the cheers given before now - CheerSettle, one per activity, for
// the owners who want them (models.Users.NotifySocial)
func CheckCheers(db models.DB, logger zerolog.Logger, now time.Time) error {
	notified, err := db.ClaimCheers(now, now.Add(-CheerSettle), func(tx *models.DB, batch models.CheerBatch) error {
		_, err := tx.NotifyUser(batch.OwnerID, models.NotifyCheers, cheersMessage(batch).Encode())
		return err
	})
	if notified > 0 {
		logger.Info().Int("activities", notified).Msg("cheers: notified")
	}
	return err
}

// shorten cuts text to max characters, with an ellipsis when cut
func shorten(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return strings.TrimSpace(string([]rune(text)[:max-1])) + "…"
}

// CommentMessage is the notification of a comment on an activity to its owner
func CommentMessage(author string, activityID uuid.UUID, title, body string) PushMessage {
	return PushMessage{Title: fmt.Sprintf("%s commented on %s", author, title),
		Body: shorten(strings.Join(strings.Fields(body), " "), commentShown), URL: activityURL(activityID)}
}

// FollowMessage is the notification of a new follower, or of their request when the user accepts each one
func FollowMessage(follower string, request bool) PushMessage {
	if request {
		return PushMessage{Title: follower + " asks to follow you", Body: "Accept or refuse on your followers page.",
			URL: "/#/followers"}
	}
	return PushMessage{Title: follower + " follows you", Body: "They see your activities not private in their feed.",
		URL: "/#/followers"}
}

// FollowAcceptedMessage is the notification of a follow request accepted, to the follower
func FollowAcceptedMessage(followee string, followeeUUID uuid.UUID) PushMessage {
	return PushMessage{Title: followee + " accepted your request", Body: "Their activities are in your feed now.",
		URL: "/#/athletes/" + followeeUUID.String()}
}

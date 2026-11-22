package notifications

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Notifs sends the notifications queued in the database to the browsers subscribed (web push)
type Notifs struct {
	PubKey  string
	PrivKey string
	// Subject is the contact of the VAPID tokens: the URL of the instance (DOMAIN)
	Subject string
	// Email sends a notification (a PushMessage, as JSON) by email to a user, nil when the emails are off:
	// the notifications of the email channels then wait, and are dropped after StaleAfter
	Email func(ctx context.Context, userID uint, message string) error
}

// SendInterval is how often the queued notifications are sent
const SendInterval = 30 * time.Second

// StaleAfter: a notification still unsent after it is dropped
const StaleAfter = 24 * time.Hour

func New() *Notifs {
	return &Notifs{}
}

// Notify sends a notification by its channel
func (notif *Notifs) Notify(notification types.NotificationsBody) error {
	switch notification.ChannelProvider {
	case models.PushProvider:
		return notif.BrowserSend(notification)
	case models.EmailProvider:
		if notif.Email == nil {
			return errors.New("the emails are off")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return notif.Email(ctx, notification.UserID, notification.Message)
	default:
		return errors.New("no sender for the channels " + notification.ChannelProvider)
	}
}

// Run sends the queued notifications every SendInterval, until ctx is done
func (notif *Notifs) Run(ctx context.Context, logger zerolog.Logger, client *http.Client, db models.DB) {
	for {
		notif.send(logger, db)
		select {
		case <-ctx.Done():
			return
		case <-time.After(SendInterval):
		}
	}
}

// send sends the queued notifications: a sent one is marked so, the channel of a gone subscription is
// removed, a failed one is tried again at the next round, until StaleAfter
func (notif *Notifs) send(logger zerolog.Logger, db models.DB) {
	notifications, err := db.GetUnsentNotifications()
	if err != nil {
		logger.Error().Err(err).Msg("failed to get unsent notifications")
		return
	}
	for _, notification := range notifications {
		err := notif.Notify(notification)
		switch {
		case errors.Is(err, ErrSubscriptionGone):
			logger.Info().Uint("channel", notification.ChannelID).Msg("push subscription gone: removed")
			if err := db.DeleteChannel(notification.ChannelID); err != nil {
				logger.Error().Err(err).Msg("failed to remove a gone push subscription")
			}
		case err != nil:
			logger.Warn().Err(err).Uint("notification", notification.ID).Msg("notification not sent, tried again later")
		default:
			if err := db.MarkAsSent(notification.ID); err != nil {
				logger.Error().Err(err).Msg("failed to mark notification as sent")
			}
		}
	}
	if dropped, err := db.DropStaleNotifications(time.Now().Add(-StaleAfter)); err != nil {
		logger.Error().Err(err).Msg("failed to drop the stale notifications")
	} else if dropped > 0 {
		logger.Warn().Int64("notifications", dropped).Dur("after", StaleAfter).Msg("notifications never sent, dropped")
	}
}

// Start loads the VAPID keys of the notifications, generating them the first time
func (notif *Notifs) Start(logger zerolog.Logger, client *http.Client, db models.DB) error {
	keys, err := db.GetKeys()
	if err != nil {
		logger.Error().Err(err).Msg("failed to get keys")
		return err
	}
	if keys == nil {
		if keys, err = notif.GenerateVAPIDKeys(logger); err != nil {
			return err
		}
		if err := db.SaveKeys(keys); err != nil {
			return err
		}
	}
	notif.PubKey = keys.PubKey
	notif.PrivKey = keys.PrivKey
	return nil
}

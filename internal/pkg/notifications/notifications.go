package notifications

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"net/http"
	"time"
)

// Generic methods for Notifications

type Notifs struct {
	PubKey  string
	PrivKey string
}

func (notif *Notifs) Notify(notification types.NotificationsBody, logger zerolog.Logger, client *http.Client, db models.DB) error {
	var err error
	switch notification.ChannelProvider {
	case "email":

	case "browser":
		err = notif.BrowserSend(notification, logger, client, db)
	case "default":

	}
	if err != nil {
		log.Error().Err(err).Msg("failed to send notification")
		return err
	}

	return nil
}

// Run sends the unsent notifications every 30 seconds, until ctx is done
func (notif *Notifs) Run(ctx context.Context, logger zerolog.Logger, client *http.Client, db models.DB) {
	for {
		logger.Debug().Msg("sending notifications")
		notif.send(client, logger, db)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// send sends the unsent notifications
func (notif *Notifs) send(client *http.Client, logger zerolog.Logger, db models.DB) {
	// get all unsent notifications
	notifications, err := db.GetUnsentNotifications()
	if err != nil {
		log.Error().Err(err).Msg("failed to get unsent notifications")
	}
	// send notifications
	for _, notification := range notifications {
		err = notif.Notify(notification, logger, client, db)
		if err != nil {
			log.Error().Err(err).Msg("failed to send notification")
		} else {
			log.Info().Msg("notification sent")
			err = db.MarkAsSent(notification.ID)
			if err != nil {
				log.Error().Err(err).Msg("failed to mark notification as sent")
			}
		}
	}
}

func New() *Notifs {

	return &Notifs{}
}

// Start loads the keys of the notifications, generating them the first time; Run sends them
func (notif *Notifs) Start(logger zerolog.Logger, client *http.Client, db models.DB) error {

	keys, err := db.GetKeys()
	if err != nil {
		log.Error().Err(err).Msg("failed to get keys")
		return err

	}
	if keys == nil {
		keys, err = notif.GenerateVAPIDKeys(logger)
		if err != nil {
			log.Error().Err(err).Msg("failed to generate keys")
			return err
		}
		err = db.SaveKeys(keys)
		notif.PubKey = keys.PubKey
		notif.PrivKey = keys.PrivKey
	} else {
		// populate the keys
		notif.PubKey = keys.PubKey
		notif.PrivKey = keys.PrivKey
	}
	return nil
}

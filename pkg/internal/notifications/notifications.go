package notifications

import (
	"MySportWeb/pkg/internal/types"
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

func (notif *Notifs) Notify(notification types.NotificationsBody, logger zerolog.Logger, client *http.Client, db database.DB) error {
	var err error
	switch notification.ChannelProvider {
	case "telegram":
		err = notif.TelegramSend(notification, logger, client)

	case "discord":
		err = notif.DiscordSend(notification)
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

func (notif *Notifs) Notifications(client *http.Client, logger zerolog.Logger, db database.DB) {
	for true {
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
		// sleep for 30 seconds
		time.Sleep(30 * time.Second)
	}
}

func New() *Notifs {

	return &Notifs{}
}

func (notif *Notifs) Start(logger zerolog.Logger, client *http.Client, db database.DB) error {

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
	go func() {
		for {
			logger.Debug().Msg("sending notifications")
			notif.Notifications(client, logger, db)
		}
	}()
	return nil
}

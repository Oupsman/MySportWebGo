package notifications

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"encoding/json"
	"errors"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/rs/zerolog"
	"net/http"
)

func (notif *Notifs) GenerateVAPIDKeys(logger zerolog.Logger) (*models.Keys, error) {
	// Generate VAPID keys
	privateVapidKeys, publicVapidKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		logger.Error().Err(err).Msg("failed to generate Vapid Keys")
		return nil, err
	}
	return &models.Keys{
		PubKey:  publicVapidKey,
		PrivKey: privateVapidKeys,
	}, nil
}
func (notif *Notifs) BrowserSend(notification types.NotificationsBody, logger zerolog.Logger, client *http.Client, db database.DB) error {

	logger.Debug().Msg("sending webpush notification")
	// Get VAPID keys
	keys, err := db.GetKeys()
	if err != nil {
		return err
	}

	s := &webpush.Subscription{}

	err = json.Unmarshal([]byte(notification.Config), s)
	if err != nil {
		return err
	}

	// Send notification
	response, err := webpush.SendNotification([]byte(notification.Message), s, &webpush.Options{
		Subscriber:      "oupsman@oupsman.fr", // Do not include "mailto:"
		VAPIDPublicKey:  keys.PubKey,
		VAPIDPrivateKey: keys.PrivKey,
		//		Topic:           "Game changed price",
		TTL: 120,
	})
	if err != nil {
		return err
	}
	if response == nil {
		return errors.New("no response from web push server")
	}
	if response.StatusCode != 201 {
		return errors.New("failed to send notification")
	}
	defer response.Body.Close()

	return nil
}

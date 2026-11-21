package notifications

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/rs/zerolog"
)

// ErrSubscriptionGone is a push subscription the push service doesn't know any more (unsubscribed, the
// browser data cleared, expired): its channel is removed
var ErrSubscriptionGone = errors.New("the push subscription is gone")

// PushTTL is how long the push service keeps a notification for a browser that is offline
const PushTTL = 24 * 60 * 60

// pushClient sends the notifications to the push services
var pushClient = &http.Client{Timeout: 30 * time.Second}

// PushMessage is what the service worker of the frontend shows (public/sw.js): a title, a text, and the
// page of the app a click opens ("/#/equipment/<id>")
type PushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// Encode returns the message as the payload of a notification
func (m PushMessage) Encode() string {
	payload, _ := json.Marshal(m)
	return string(payload)
}

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

// BrowserSend sends a notification to the push service of a browser subscription. ErrSubscriptionGone
// when the service says the subscription doesn't exist any more.
func (notif *Notifs) BrowserSend(notification types.NotificationsBody) error {
	s := &webpush.Subscription{}
	if err := json.Unmarshal([]byte(notification.Config), s); err != nil {
		return err
	}
	response, err := webpush.SendNotification([]byte(notification.Message), s, &webpush.Options{
		HTTPClient: pushClient,
		// who to contact about these notifications: the URL of the instance (an https: URL is allowed)
		Subscriber:      notif.Subject,
		VAPIDPublicKey:  notif.PubKey,
		VAPIDPrivateKey: notif.PrivKey,
		TTL:             PushTTL,
		Urgency:         webpush.UrgencyNormal,
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return ErrSubscriptionGone
	case response.StatusCode < 200 || response.StatusCode > 299:
		return fmt.Errorf("the push service answered %s", response.Status)
	}
	return nil
}

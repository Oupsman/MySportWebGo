package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/notifications"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// pushServices are the hosts of the push services of the browsers (Chrome and the Chromium ones, Firefox,
// Safari, Edge), and their subdomains: the backend posts the notifications to the endpoint a browser gives,
// any other host would let a user make the backend send requests where they choose (SSRF)
var pushServices = []string{"fcm.googleapis.com", "android.googleapis.com", "push.services.mozilla.com",
	"push.apple.com", "notify.windows.com"}

// validPushEndpoint tells whether an endpoint is an https URL of a push service
func validPushEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, service := range pushServices {
		if host == service || strings.HasSuffix(host, "."+service) {
			return true
		}
	}
	return false
}

// validPushKey tells whether a key of a subscription is base64 (URL alphabet) of the expected size: 65
// bytes for p256dh (a P-256 point), 16 for auth
func validPushKey(key string, size int) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(key, "="))
	return err == nil && len(decoded) == size
}

// pushSubscription is the PushSubscription of a browser, as its toJSON() gives it
type pushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// GetPush returns the public key the browsers subscribe with, and the push subscriptions of the user
func GetPush(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	channels, err := App.DB.GetPushChannels(middlewares.CurrentUser(c).ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"public_key": App.Notifications.PubKey, "subscriptions": channels})
}

// Subscribe records the push subscription of a browser of the user: {subscription, name}
func Subscribe(c *gin.Context) {
	var body struct {
		Subscription pushSubscription `json:"subscription"`
		Name         string           `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	s := body.Subscription
	if !validPushEndpoint(s.Endpoint) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not the endpoint of a known push service"})
		return
	}
	if !validPushKey(s.Keys.P256dh, 65) || !validPushKey(s.Keys.Auth, 16) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid subscription keys"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if len(name) > 100 {
		name = name[:100]
	}
	config, err := json.Marshal(s)
	if err != nil {
		internalError(c, err)
		return
	}
	App := c.MustGet("App").(*app.App)
	channel, err := App.DB.SavePushChannel(middlewares.CurrentUser(c).ID, s.Endpoint, string(config), name)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscription": channel})
}

// Unsubscribe removes a push subscription of the user
func Unsubscribe(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		badRequest(c, err)
		return
	}
	App := c.MustGet("App").(*app.App)
	found, err := App.DB.DeletePushChannel(middlewares.CurrentUser(c).ID, uint(id))
	if err != nil {
		internalError(c, err)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such subscription"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "unsubscribed"})
}

// TestPush queues a test notification for the browsers of the user: sent within notifications.SendInterval
func TestPush(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	message := notifications.PushMessage{Title: "MySportWeb", Body: "Notifications work on this device.", URL: "/#/profile/view"}
	queued, err := App.DB.NotifyUser(middlewares.CurrentUser(c).ID, message.Encode())
	if err != nil {
		internalError(c, err)
		return
	}
	if queued == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "no browser is subscribed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"queued": queued})
}

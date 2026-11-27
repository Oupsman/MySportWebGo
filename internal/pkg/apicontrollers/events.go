package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
)

// the user events and notifications an admin page shows at most
const (
	defaultLogPage = 100
	maxLogPage     = 500
)

// recordEvent saves an event of user (nil for one deleted), from the IP of the request. A failure is only
// logged: the request goes on without it.
func recordEvent(c *gin.Context, user models.Users, kind, detail string) {
	App := c.MustGet("App").(*app.App)
	event := models.UserEvent{Username: user.Username, Kind: kind, Detail: detail, IP: c.ClientIP()}
	if user.ID != 0 {
		id := user.ID
		event.UserID = &id
	}
	if err := App.DB.RecordEvent(&event); err != nil {
		App.Logger.Error().Err(err).Str("kind", kind).Uint("user_id", user.ID).Msg("user event not recorded")
	}
}

// recordAdminEvent saves an event of user done by the admin of the request
func recordAdminEvent(c *gin.Context, user models.Users, kind, detail string) {
	by := "by " + middlewares.CurrentUser(c).Username
	if detail != "" {
		by = detail + ", " + by
	}
	recordEvent(c, user, kind, by)
}

// logQuery reads the filter of an admin log: user_id, kind (one of kinds), before (an id: the next page),
// limit. False, with the answer sent, when invalid.
func logQuery(c *gin.Context, kinds []string) (userID *uint, kind string, before uint64, limit int, ok bool) {
	bad := func(message string) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": message})
	}
	if raw := c.Query("user_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			bad("invalid user_id")
			return
		}
		u := uint(id)
		userID = &u
	}
	kind = c.Query("kind")
	if kind != "" && !slices.Contains(kinds, kind) {
		bad("unknown kind")
		return
	}
	if raw := c.Query("before"); raw != "" {
		var err error
		if before, err = strconv.ParseUint(raw, 10, 63); err != nil {
			bad("invalid before")
			return
		}
	}
	limit = defaultLogPage
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLogPage {
			bad("limit is 1 to " + strconv.Itoa(maxLogPage))
			return
		}
		limit = n
	}
	return userID, kind, before, limit, true
}

// GetUserEvents answers the user events, newest first:
// ?user_id=<id>&kind=<kind>&before=<id of the last event of the previous page>&limit=<n>
func GetUserEvents(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	userID, kind, before, limit, ok := logQuery(c, models.EventKinds)
	if !ok {
		return
	}
	events, err := App.DB.GetEvents(models.EventFilter{UserID: userID, Kind: kind, Before: int64(before), Limit: limit})
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "kinds": models.EventKinds,
		"retention_days": int(models.EventRetention.Hours() / 24)})
}

// GetNotificationLogs answers the notifications of the users, newest first: when queued and sent, to whom,
// through which channel and about what, never their message. Same filter as GetUserEvents.
func GetNotificationLogs(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	userID, kind, before, limit, ok := logQuery(c, models.NotifyKinds)
	if !ok {
		return
	}
	logs, err := App.DB.GetNotificationLogs(models.NotificationFilter{UserID: userID, Kind: kind, Before: uint(before),
		Limit: limit})
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"notifications": logs, "kinds": models.NotifyKinds})
}

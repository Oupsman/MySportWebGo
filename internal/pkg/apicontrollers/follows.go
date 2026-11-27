package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/services/activityService"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The follows and the feed: a logged in user finds the others by their username, sees their profile (their
// activities they may see), and follows them; a user who accepts each follower (Users.FollowApproval, on by
// default) gets a request first. The feed is the activities of the users followed, as their visibility lets
// the follower see them. All of it is for the sessions of the web application, never the device tokens, and
// never shows the ids nor the emails of the users.

const (
	// searchMinLength, searchLimit: a search of the users is 2 characters at least, and answers 20 at most
	searchMinLength = 2
	searchLimit     = 20
	// feedPageDefault, feedPageMax are the activities of a page of the feed or of a profile
	feedPageDefault = 20
	feedPageMax     = 50
	// followsPerHour is the most follow requests a user sends in an hour
	followsPerHour = 30
)

// followLimiter limits the follow requests of each user: a refused request can be sent again, not flooded
var followLimiter = middlewares.NewRateLimiter(followsPerHour, time.Hour)

// SearchUsers answers the users whose username contains ?q=, with the state of their follow by the user
func SearchUsers(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	q := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(q) < searchMinLength {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "search at least 2 characters of a username"})
		return
	}
	users, err := db.SearchUsers(middlewares.CurrentUser(c).ID, q, searchLimit)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// pathUser returns the enabled user of the :uuid parameter, aborting the request on error: 404 for a
// disabled one, as an unknown one
func pathUser(c *gin.Context) (models.Users, bool) {
	db := c.MustGet("App").(*app.App).DB
	id, err := uuid.Parse(c.Param("uuid"))
	if err != nil {
		badRequest(c, err)
		return models.Users{}, false
	}
	user, err := db.GetUserByUUID(id)
	if err == nil && user.DisabledAt != nil {
		err = gorm.ErrRecordNotFound
	}
	if err != nil {
		lookupError(c, err, "user")
		return user, false
	}
	return user, true
}

// GetProfile answers a user as the user of the request sees them: their follow counts, whether they follow
// them and whether they are followed back
func GetProfile(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	viewer := middlewares.CurrentUser(c)
	user, ok := pathUser(c)
	if !ok {
		return
	}
	counts, err := db.GetFollowCounts(user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	state, followsYou := models.FollowNone, models.FollowNone
	if user.ID != viewer.ID {
		if state, err = db.GetFollowState(viewer.ID, user.ID); err == nil {
			followsYou, err = db.GetFollowState(user.ID, viewer.ID)
		}
		if err != nil {
			internalError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"user": models.SocialUser{UUID: user.UUID, Username: user.Username},
		"me": user.ID == viewer.ID, "state": state, "follows_you": followsYou == models.FollowFollowing,
		"counts": counts})
}

// feedCursor reads the page asked by ?before= (RFC 3339) and ?before_id=, and ?limit=; it aborts the request
// on error
func feedCursor(c *gin.Context) (models.FeedCursor, int, bool) {
	var cursor models.FeedCursor
	limit := feedPageDefault
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > feedPageMax {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "limit is between 1 and 50"})
			return cursor, 0, false
		}
		limit = n
	}
	if c.Query("before") == "" {
		return cursor, limit, true
	}
	date, err := time.Parse(time.RFC3339Nano, c.Query("before"))
	if err == nil {
		cursor.ID, err = uuid.Parse(c.Query("before_id"))
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid before or before_id"})
		return cursor, 0, false
	}
	cursor.Date = date
	return cursor, limit, true
}

// answerFeed answers a page of activities, with the cursor of the next one (nil after the last one)
func answerFeed(c *gin.Context, items []models.FeedItem, limit int) {
	for i := range items {
		if items[i].HasMap {
			items[i].Thumbnail = activityService.ThumbnailURL(items[i].Owner.UUID, items[i].ID)
		}
	}
	var next gin.H
	if len(items) == limit {
		last := items[len(items)-1]
		next = gin.H{"before": last.Date.Format(time.RFC3339Nano), "before_id": last.ID}
	}
	c.JSON(http.StatusOK, gin.H{"activities": items, "next": next})
}

// GetProfileActivities answers the activities of a user the user of the request may see
func GetProfileActivities(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, ok := pathUser(c)
	if !ok {
		return
	}
	cursor, limit, ok := feedCursor(c)
	if !ok {
		return
	}
	items, err := db.GetProfileActivities(user.ID, middlewares.CurrentUser(c).ID, cursor, limit)
	if err != nil {
		internalError(c, err)
		return
	}
	answerFeed(c, items, limit)
}

// GetFeed answers the activities of the users the user follows, the most recent first
func GetFeed(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	cursor, limit, ok := feedCursor(c)
	if !ok {
		return
	}
	items, err := db.GetFeed(middlewares.CurrentUser(c).ID, cursor, limit)
	if err != nil {
		internalError(c, err)
		return
	}
	answerFeed(c, items, limit)
}

// notifySocial queues a notification for a user who wants those of the social side, logging a failure: the
// request succeeds without it
func notifySocial(App *app.App, to models.Users, message notifications.PushMessage) {
	if !to.NotifySocial {
		return
	}
	if _, err := App.DB.NotifyUser(to.ID, models.NotifyFollow, message.Encode()); err != nil {
		App.Logger.Error().Err(err).Uint("user_id", to.ID).Msg("follow not notified")
	}
}

// Follow follows a user: a request when they accept each follower, else at once; following again changes
// nothing. It answers the state of the follow.
func Follow(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	follower := middlewares.CurrentUser(c)
	followee, ok := pathUser(c)
	if !ok {
		return
	}
	if followee.ID == follower.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "you can't follow yourself"})
		return
	}
	state, err := App.DB.GetFollowState(follower.ID, followee.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	if state == models.FollowNone {
		if allowed, wait := followLimiter.Allow(strconv.FormatUint(uint64(follower.ID), 10)); !allowed {
			middlewares.TooManyRequests(c, wait)
			return
		}
	}
	state, created, err := App.DB.RequestFollow(follower.ID, followee.ID, !followee.FollowApproval)
	if err != nil {
		internalError(c, err)
		return
	}
	if created {
		notifySocial(App, followee, notifications.FollowMessage(follower.Username, state == models.FollowPending))
	}
	c.JSON(http.StatusOK, gin.H{"state": state})
}

// Unfollow stops following a user, or takes back the request
func Unfollow(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	followee, ok := pathUser(c)
	if !ok {
		return
	}
	if _, err := db.RemoveFollow(middlewares.CurrentUser(c).ID, followee.ID); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": models.FollowNone})
}

// answerFollows answers the followers of the user, the users they follow (with the requests of both) and
// whether they accept each follower
func answerFollows(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	followers, err := db.GetFollows(user.ID, true)
	if err != nil {
		internalError(c, err)
		return
	}
	following, err := db.GetFollows(user.ID, false)
	if err != nil {
		internalError(c, err)
		return
	}
	// read again: SetFollowApproval changes it
	fresh, err := db.GetUser(uint64(user.ID))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"followers": followers, "following": following, "approval": fresh.FollowApproval})
}

// GetFollows answers the followers and the followees of the user
func GetFollows(c *gin.Context) {
	answerFollows(c)
}

// SetFollowApproval turns on or off the approval of each follower, {"enabled": true}: off accepts the
// requests waiting
func SetFollowApproval(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if body.Enabled == nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "enabled is missing"})
		return
	}
	if err := db.SetFollowApproval(middlewares.CurrentUser(c).ID, *body.Enabled); err != nil {
		internalError(c, err)
		return
	}
	answerFollows(c)
}

// AcceptFollower accepts the request of a user to follow the user of the request; the follower is notified
func AcceptFollower(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	follower, ok := pathUser(c)
	if !ok {
		return
	}
	err := App.DB.AcceptFollower(user.ID, follower.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "no request of this user"})
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	notifySocial(App, follower, notifications.FollowAcceptedMessage(user.Username, user.UUID))
	answerFollows(c)
}

// RemoveFollower refuses the request of a user, or stops them following the user of the request; they
// aren't told
func RemoveFollower(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	follower, ok := pathUser(c)
	if !ok {
		return
	}
	if _, err := db.RemoveFollow(follower.ID, middlewares.CurrentUser(c).ID); err != nil {
		internalError(c, err)
		return
	}
	answerFollows(c)
}

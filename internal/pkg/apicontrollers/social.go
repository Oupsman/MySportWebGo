package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/internal/pkg/utils"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// The cheers and the comments: a logged in user cheers and comments the activities they may see (mayView),
// the others see nothing of them. A user doesn't cheer their own activities, but comments them (to answer).
// Only the activities their owner opened to comments (Activity.CanComments, off by default) take new ones;
// closed again, the comments stay, and can be deleted but not changed. The author of a comment changes or
// deletes it; the owner of the activity and the admins delete it.

const (
	// maxCommentLength is the most characters of a comment, as the database checks
	maxCommentLength = 2000
	// maxCommentsPerActivity bounds the comments of an activity
	maxCommentsPerActivity = 500
	// commentsPerMinute is the most comments a user posts in a minute
	commentsPerMinute = 10
)

// commentLimiter limits the comments of each user
var commentLimiter = middlewares.NewRateLimiter(commentsPerMinute, time.Minute)

// socialActivity returns the activity of the :id parameter and the user of the request, when they may see
// it; it aborts the request otherwise
func socialActivity(c *gin.Context) (models.ActivityAccess, models.Users, bool) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return models.ActivityAccess{}, user, false
	}
	access, err := db.GetActivityAccess(id)
	if err != nil {
		lookupError(c, err, "activity")
		return access, user, false
	}
	if !mayView(access.Visibility, access.OwnerID == user.ID, true) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You can't access this activity"})
		return access, user, false
	}
	return access, user, true
}

// socialComment is a comment as a user sees it: whether it is theirs, and whether they may delete it
type socialComment struct {
	models.CommentView
	Mine      bool `json:"mine"`
	CanDelete bool `json:"can_delete"`
}

// mayDeleteComment tells whether user may delete a comment of userID on an activity of ownerID
func mayDeleteComment(user models.Users, authorID, ownerID uint) bool {
	return user.ID == authorID || user.ID == ownerID || user.Role == models.RoleAdmin
}

// answerSocial answers the cheers and the comments of an activity as user sees them
func answerSocial(c *gin.Context, status int, access models.ActivityAccess, user models.Users) {
	db := c.MustGet("App").(*app.App).DB
	cheers, err := db.GetCheers(access.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	views, err := db.GetComments(access.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	cheered := false
	for _, cheer := range cheers {
		cheered = cheered || cheer.UserID == user.ID
	}
	comments := make([]socialComment, len(views))
	for i, v := range views {
		comments[i] = socialComment{CommentView: v, Mine: v.UserID == user.ID,
			CanDelete: mayDeleteComment(user, v.UserID, access.OwnerID)}
	}
	c.JSON(status, gin.H{"cheers": cheers, "cheered": cheered, "can_cheer": access.OwnerID != user.ID,
		"can_comment": access.CanComments, "comments": comments})
}

// GetActivitySocial answers the cheers and the comments of an activity
func GetActivitySocial(c *gin.Context) {
	access, user, ok := socialActivity(c)
	if !ok {
		return
	}
	answerSocial(c, http.StatusOK, access, user)
}

// CheerActivity cheers an activity of another user; cheering again changes nothing
func CheerActivity(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	access, user, ok := socialActivity(c)
	if !ok {
		return
	}
	if access.OwnerID == user.ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "you can't cheer your own activity"})
		return
	}
	if err := db.AddCheer(access.ID, user.ID); err != nil {
		internalError(c, err)
		return
	}
	answerSocial(c, http.StatusOK, access, user)
}

// UncheerActivity takes back the cheer of the user on an activity, if any
func UncheerActivity(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	access, user, ok := socialActivity(c)
	if !ok {
		return
	}
	if err := db.RemoveCheer(access.ID, user.ID); err != nil {
		internalError(c, err)
		return
	}
	answerSocial(c, http.StatusOK, access, user)
}

// commentsOpen tells whether an activity takes comments, answering 403 when not
func commentsOpen(c *gin.Context, access models.ActivityAccess) bool {
	if !access.CanComments {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "comments are off on this activity"})
	}
	return access.CanComments
}

// commentBody returns the text of a comment of the request, checked
func commentBody(c *gin.Context) (string, bool) {
	var input struct {
		Body string `json:"body"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, err)
		return "", false
	}
	body := strings.TrimSpace(input.Body)
	switch {
	case body == "":
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "the comment is empty"})
		return "", false
	case utf8.RuneCountInString(body) > maxCommentLength:
		c.AbortWithStatusJSON(http.StatusBadRequest,
			gin.H{"error": fmt.Sprintf("a comment is %d characters at most", maxCommentLength)})
		return "", false
	case strings.ContainsRune(body, 0):
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid character in the comment"})
		return "", false
	}
	return body, true
}

// CreateComment comments an activity, {"body": "..."}: its owner is notified when it isn't theirs
func CreateComment(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	access, user, ok := socialActivity(c)
	if !ok || !commentsOpen(c, access) {
		return
	}
	body, ok := commentBody(c)
	if !ok {
		return
	}
	count, err := db.CountComments(access.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	if count >= maxCommentsPerActivity {
		c.AbortWithStatusJSON(http.StatusConflict,
			gin.H{"error": fmt.Sprintf("an activity has %d comments at most", maxCommentsPerActivity)})
		return
	}
	if allowed, wait := commentLimiter.Allow(strconv.FormatUint(uint64(user.ID), 10)); !allowed {
		middlewares.TooManyRequests(c, wait)
		return
	}
	comment := models.Comment{ActivityID: access.ID, UserID: user.ID, Body: body}
	if err := db.CreateComment(&comment); err != nil {
		internalError(c, err)
		return
	}
	if access.OwnerID != user.ID && access.OwnerNotifySocial {
		message := notifications.CommentMessage(user.Username, access.ID, access.Title, body)
		if _, err := db.NotifyUser(access.OwnerID, models.NotifyComment, message.Encode()); err != nil {
			// the comment is saved: the request succeeds without its notification
			App.Logger.Error().Err(err).Str("activity", access.ID.String()).Msg("comment not notified")
		}
	}
	answerSocial(c, http.StatusCreated, access, user)
}

// activityComment returns the comment of the :comment parameter of the activity, aborting the request on error
func activityComment(c *gin.Context, access models.ActivityAccess) (models.Comment, bool) {
	db := c.MustGet("App").(*app.App).DB
	id, err := utils.GetUUID(c.Param("comment"))
	if err != nil {
		badRequest(c, err)
		return models.Comment{}, false
	}
	comment, err := db.GetComment(access.ID, id)
	if err != nil {
		lookupError(c, err, "comment")
		return comment, false
	}
	return comment, true
}

// UpdateComment changes the text of a comment of the user, {"body": "..."}
func UpdateComment(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	access, user, ok := socialActivity(c)
	if !ok {
		return
	}
	comment, ok := activityComment(c, access)
	if !ok {
		return
	}
	if comment.UserID != user.ID {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "only its author changes a comment"})
		return
	}
	if !commentsOpen(c, access) {
		return
	}
	body, ok := commentBody(c)
	if !ok {
		return
	}
	if err := db.UpdateComment(&comment, body); err != nil {
		internalError(c, err)
		return
	}
	answerSocial(c, http.StatusOK, access, user)
}

// DeleteComment deletes a comment: of the user, on their activity, or as an admin
func DeleteComment(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	access, user, ok := socialActivity(c)
	if !ok {
		return
	}
	comment, ok := activityComment(c, access)
	if !ok {
		return
	}
	if !mayDeleteComment(user, comment.UserID, access.OwnerID) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "you can't delete this comment"})
		return
	}
	if err := App.DB.DeleteComment(comment.ID); err != nil {
		internalError(c, err)
		return
	}
	if comment.UserID != user.ID {
		App.Logger.Info().Uint("user_id", user.ID).Uint("author_id", comment.UserID).
			Str("activity", access.ID.String()).Msg("comment of another user deleted")
	}
	answerSocial(c, http.StatusOK, access, user)
}

// SocialNotifications answers whether the user is notified of the cheers and comments on their activities
func SocialNotifications(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": middlewares.CurrentUser(c).NotifySocial})
}

// SetSocialNotifications turns the notifications of the cheers and comments on or off, {"enabled": true}
func SetSocialNotifications(c *gin.Context) {
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
	if err := db.SetNotifySocial(middlewares.CurrentUser(c).ID, *body.Enabled); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": *body.Enabled})
}

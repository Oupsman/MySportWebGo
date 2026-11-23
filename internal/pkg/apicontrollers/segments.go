package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/services/activityService"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateSegment creates a segment from a stretch of an activity of the user: {activity_id, start, end, name},
// start and end indexes of its GPS points. Its passes are found in the activities of the user at once.
func CreateSegment(c *gin.Context) {
	var body struct {
		ActivityID uuid.UUID `json:"activity_id" binding:"required"`
		Start      *int      `json:"start" binding:"required"`
		End        *int      `json:"end" binding:"required"`
		Name       string    `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	track, err := db.GetSegmentTrack(body.ActivityID)
	if err == nil && track.UserID != user.ID {
		// another user's activity: as if it didn't exist
		err = gorm.ErrRecordNotFound
	}
	if err != nil {
		lookupError(c, err, "activity")
		return
	}
	segment, err := activityService.NewSegment(track, *body.Start, *body.End, strings.TrimSpace(body.Name))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.CreateSegment(&segment); err != nil {
		internalError(c, err)
		return
	}
	efforts, err := activityService.MatchSegmentActivities(&db, segment, activityService.MatchCreate)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"segment": segment, "efforts": efforts})
}

// GetSegments returns the segments of the user with their passes
func GetSegments(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	segments, err := db.GetSegmentsList(middlewares.CurrentUser(c).ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"segments": segments})
}

// segmentParam returns the segment of the :id of the route, of the user; false when answered
func segmentParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return uuid.Nil, false
	}
	return id, true
}

// GetSegment returns a segment of the user with its passes, the fastest first
func GetSegment(c *gin.Context) {
	id, ok := segmentParam(c)
	if !ok {
		return
	}
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	segment, err := db.GetSegment(user.ID, id)
	if err != nil {
		lookupError(c, err, "segment")
		return
	}
	efforts, err := db.GetSegmentEfforts(user.ID, id)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"segment": segment, "efforts": efforts})
}

// RenameSegment renames a segment of the user: {name}
func RenameSegment(c *gin.Context) {
	id, ok := segmentParam(c)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	name := strings.TrimSpace(body.Name)
	if n := len([]rune(name)); n == 0 || n > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": activityService.ErrSegmentNameSize.Error()})
		return
	}
	db := c.MustGet("App").(*app.App).DB
	found, err := db.RenameSegment(middlewares.CurrentUser(c).ID, id, name)
	if err == nil && !found {
		err = gorm.ErrRecordNotFound
	}
	if err != nil {
		lookupError(c, err, "segment")
		return
	}
	c.JSON(http.StatusOK, gin.H{"name": name})
}

// DeleteSegment deletes a segment of the user and its passes
func DeleteSegment(c *gin.Context) {
	id, ok := segmentParam(c)
	if !ok {
		return
	}
	db := c.MustGet("App").(*app.App).DB
	found, err := db.DeleteSegment(middlewares.CurrentUser(c).ID, id)
	if err == nil && !found {
		err = gorm.ErrRecordNotFound
	}
	if err != nil {
		lookupError(c, err, "segment")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "segment deleted"})
}

// GetActivitySegments returns the passes of an activity of the user on their segments, ranked on each
func GetActivitySegments(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	db := c.MustGet("App").(*app.App).DB
	efforts, err := db.GetActivitySegmentEfforts(middlewares.CurrentUser(c).ID, id)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"efforts": efforts})
}

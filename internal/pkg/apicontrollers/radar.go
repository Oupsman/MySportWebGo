package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SetRadarEventsShown sets whether the vehicles of the rear radar are drawn on the maps of the activities of
// the user: {"shown": true}. A switch of the map, without the password the profile asks; GET /user tells it
// (show_radar_events).
func SetRadarEventsShown(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	var body struct {
		Shown *bool `json:"shown"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Shown == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `{"shown": true or false} expected`})
		return
	}
	user := middlewares.CurrentUser(c)
	if err := db.Model(&models.Users{}).Where("id = ?", user.ID).Update("show_radar_events", *body.Shown).Error; err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"shown": *body.Shown})
}

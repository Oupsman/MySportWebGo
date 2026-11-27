package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// SetRadarEventsShown sets which vehicles of the rear radar are drawn on the maps of the activities of the
// user: {"approaching": true, "fast": false}, either or both, a missing one unchanged. Switches of the map,
// without the password the profile asks; GET /user tells them (show_radar_approaching, show_radar_fast).
func SetRadarEventsShown(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	var body struct {
		Approaching *bool `json:"approaching"`
		Fast        *bool `json:"fast"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || (body.Approaching == nil && body.Fast == nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": `{"approaching": true or false, "fast": true or false} expected`})
		return
	}
	fields := map[string]any{}
	if body.Approaching != nil {
		fields["show_radar_approaching"] = *body.Approaching
	}
	if body.Fast != nil {
		fields["show_radar_fast"] = *body.Fast
	}
	user := middlewares.CurrentUser(c)
	if err := db.Model(&models.Users{}).Where("id = ?", user.ID).Updates(fields).Error; err != nil {
		internalError(c, err)
		return
	}
	var shown models.Users
	if err := db.Select("show_radar_approaching", "show_radar_fast").First(&shown, user.ID).Error; err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"approaching": shown.ShowRadarApproaching, "fast": shown.ShowRadarFast})
}

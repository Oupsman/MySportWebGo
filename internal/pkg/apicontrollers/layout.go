package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetDashboardLayout answers the widgets of the dashboard of the user: {"widgets": [...]}, null for the
// default layout
func GetDashboardLayout(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	widgets, err := db.GetDashboardLayout(middlewares.CurrentUser(c).ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"widgets": widgets})
}

// SetDashboardLayout saves the widgets of the dashboard of the user: {"widgets": [...]}, null to go back to
// the default layout
func SetDashboardLayout(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	var body struct {
		Widgets []models.DashboardWidget `json:"widgets"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if body.Widgets != nil {
		if err := models.ValidateDashboardLayout(body.Widgets); err != nil {
			badRequest(c, err)
			return
		}
	}
	if err := db.SetDashboardLayout(middlewares.CurrentUser(c).ID, body.Widgets); err != nil {
		internalError(c, errors.Join(errors.New("saving the dashboard layout"), err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"widgets": body.Widgets})
}

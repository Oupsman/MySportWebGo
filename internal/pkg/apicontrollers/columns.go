package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetActivitiesColumns answers the columns of the activities table of the user: {"columns": [...]}, null
// for the default ones
func GetActivitiesColumns(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	columns, err := db.GetActivitiesColumns(middlewares.CurrentUser(c).ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"columns": columns})
}

// SetActivitiesColumns saves the columns of the activities table of the user: {"columns": [...]}, null to
// go back to the default ones
func SetActivitiesColumns(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	var body struct {
		Columns []string `json:"columns"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if body.Columns != nil {
		if err := models.ValidateActivitiesColumns(body.Columns); err != nil {
			badRequest(c, err)
			return
		}
	}
	if err := db.SetActivitiesColumns(middlewares.CurrentUser(c).ID, body.Columns); err != nil {
		internalError(c, errors.Join(errors.New("saving the activities columns"), err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"columns": body.Columns})
}

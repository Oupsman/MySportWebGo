package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	maxWorkplaceName = 100
	maxWorkplaces    = 20
)

// workplaceInput is what the user sets of a workplace: its center and its diameter in meters
type workplaceInput struct {
	Name     string  `json:"name"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Diameter float64 `json:"diameter"`
}

// apply checks the input and copies it to workplace
func (in workplaceInput) apply(workplace *models.Workplace) error {
	name := strings.TrimSpace(in.Name)
	switch {
	case len([]rune(name)) > maxWorkplaceName:
		return fmt.Errorf("the name of a workplace is %d characters at most", maxWorkplaceName)
	case math.IsNaN(in.Lat) || in.Lat < -90 || in.Lat > 90 || math.IsNaN(in.Lon) || in.Lon < -180 || in.Lon > 180:
		return errors.New("invalid position")
	case !(in.Diameter >= activityService.WorkplaceMinDiameter && in.Diameter <= activityService.WorkplaceMaxDiameter):
		return fmt.Errorf("the diameter is between %.0f and %.0f meters", activityService.WorkplaceMinDiameter,
			activityService.WorkplaceMaxDiameter)
	}
	workplace.Name = name
	workplace.Lat = in.Lat
	workplace.Lon = in.Lon
	workplace.Diameter = math.Round(in.Diameter)
	return nil
}

// userWorkplace returns the workplace of the :id parameter of the user of the request; it aborts the request
// on error
func userWorkplace(c *gin.Context) (models.Workplace, bool) {
	db := c.MustGet("App").(*app.App).DB
	id, err := utils.GetUUID(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return models.Workplace{}, false
	}
	workplace, err := db.GetWorkplace(middlewares.CurrentUser(c).ID, id)
	if err != nil {
		lookupError(c, err, "workplace")
		return workplace, false
	}
	return workplace, true
}

// GetWorkplaces answers the workplaces of the user
func GetWorkplaces(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	workplaces, err := db.GetWorkplaces(middlewares.CurrentUser(c).ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, workplaces)
}

// saveWorkplace checks the input of the request, saves it to workplace and answers it
func saveWorkplace(c *gin.Context, workplace models.Workplace, status int) {
	db := c.MustGet("App").(*app.App).DB
	var input workplaceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, err)
		return
	}
	if err := input.apply(&workplace); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.SaveWorkplace(&workplace); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(status, workplace)
}

func CreateWorkplace(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	workplaces, err := db.GetWorkplaces(user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	if len(workplaces) >= maxWorkplaces {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%d workplaces at most", maxWorkplaces)})
		return
	}
	saveWorkplace(c, models.Workplace{UserID: user.ID}, http.StatusCreated)
}

func UpdateWorkplace(c *gin.Context) {
	workplace, ok := userWorkplace(c)
	if !ok {
		return
	}
	saveWorkplace(c, workplace, http.StatusOK)
}

// DeleteWorkplace deletes a workplace: the activities it made commutes stay commutes
func DeleteWorkplace(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	workplace, ok := userWorkplace(c)
	if !ok {
		return
	}
	if err := db.DeleteWorkplace(workplace.UserID, workplace.ID); err != nil {
		lookupError(c, err, "workplace")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "workplace deleted"})
}

// ApplyWorkplace makes the activities imported before that start or end at the workplace commutes
func ApplyWorkplace(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	workplace, ok := userWorkplace(c)
	if !ok {
		return
	}
	updated, err := activityService.ApplyWorkplace(&db, workplace.UserID, workplace)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

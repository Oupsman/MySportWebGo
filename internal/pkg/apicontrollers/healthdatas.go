package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strconv"
)

func ImportHealthDatas(c *gin.Context) {
	// TODO: CSV column mapping (Date/Weight/fatpc/musclepc/resthr/vo2max) is not
	// defined yet, so row parsing and persistence are not implemented.
	c.JSON(http.StatusNotImplemented, gin.H{"error": "health data import is not implemented yet"})
}

func CreateHealthData(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	var healthData models.HealthData
	err := c.ShouldBindJSON(&healthData)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// set after binding, so the request body can't choose them
	healthData.ID = 0
	healthData.UserID = user.ID
	if err := healthData.Validate(); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err = db.CreateHealthData(&healthData)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, healthData)
}

// healthDataOwned returns the measure of the :id parameter when it belongs to the user of the request.
// It aborts the request on error.
func healthDataOwned(c *gin.Context) (models.HealthData, bool) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return models.HealthData{}, false
	}
	healthData, err := db.GetHealthData(user.ID, uint(id))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// same answer whether the measure exists or not
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "health data not found"})
		return models.HealthData{}, false
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return models.HealthData{}, false
	}
	return healthData, true
}

func UpdateHealthData(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	healthData, ok := healthDataOwned(c)
	if !ok {
		return
	}
	var update models.HealthData
	if err := c.ShouldBindJSON(&update); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// only the measured values, ownership and id come from the database
	update.ID = healthData.ID
	update.UserID = healthData.UserID
	update.CreatedAt = healthData.CreatedAt
	if err := update.Validate(); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.UpdateHealthData(&update); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, update)
}

func DeleteHealthData(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	healthData, ok := healthDataOwned(c)
	if !ok {
		return
	}
	if err := db.DeleteHealthData(healthData.UserID, healthData.ID); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "health data deleted"})
}

func GetHealthDatas(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	healthDatas, err := db.GetHealthDatas(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, healthDatas)
}

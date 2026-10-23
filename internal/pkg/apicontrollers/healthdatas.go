package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/health"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"time"
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

// weightEstimate returns when the user reaches their target weight at the pace of their measures
func weightEstimate(db *models.DB, user models.Users) (health.Estimate, error) {
	datas, err := db.GetHealthDatas(user.ID)
	if err != nil {
		return health.Estimate{}, err
	}
	measures := make([]health.Measure, len(datas))
	for i, d := range datas {
		measures[i] = health.Measure{Date: d.Date, Weight: d.Weight}
	}
	return health.EstimateWeight(measures, user.WeightObjective, time.Now()), nil
}

// GetWeightTarget returns the target weight and when it is reached at the pace of the recent measures
func GetWeightTarget(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	estimate, err := weightEstimate(&db, middlewares.CurrentUser(c))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, estimate)
}

// SetWeightTarget sets the target weight, {"target": 72.5}, 0 removes it
func SetWeightTarget(c *gin.Context) {
	var body struct {
		Target *float64 `json:"target" binding:"required"`
	}
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if *body.Target != 0 && (*body.Target < 20 || *body.Target > 500) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "the target weight is in kg, between 20 and 500"})
		return
	}
	if err := db.SetWeightObjective(user.ID, *body.Target); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	user.WeightObjective = *body.Target
	estimate, err := weightEstimate(&db, user)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, estimate)
}

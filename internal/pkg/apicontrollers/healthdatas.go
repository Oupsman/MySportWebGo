package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

func ImportHealthDatas(c *gin.Context) {
	// TODO: CSV column mapping (Date/Weight/fatpc/musclepc/resthr/vo2max) is not
	// defined yet, so row parsing and persistence are not implemented.
	c.JSON(http.StatusNotImplemented, gin.H{"error": "health data import is not implemented yet"})
}

func CreateHealthData(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	userUUID, err := utils.GetUserUUID(c.Request.Header.Get("Authorization"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var healthData models.HealthData
	err = c.ShouldBindJSON(&healthData)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	healthData.UserID = user.ID
	if healthData.Date.IsZero() {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing date"})
		return
	}
	err = db.CreateHealthData(&healthData)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, healthData)
}

func GetHealthDatas(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	healthDatas, err := db.GetHealthDatas(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, healthDatas)
}

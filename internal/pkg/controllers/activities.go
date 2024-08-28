package controllers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"github.com/gin-gonic/gin"
	"net/http"
)

func AddActivity(c *gin.Context) {
	app := c.MustGet("App").(*app.App)
	activity := models.Activity{}
	err := c.BindJSON(&activity)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	//activity.UserID = user.ID
	err = app.DB.Create(&activity).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, activity)
}

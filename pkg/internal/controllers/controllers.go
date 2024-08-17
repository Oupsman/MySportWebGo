package controllers

import (
	"MySportWeb/pkg/internal/app"
	"github.com/gin-gonic/gin"
	"net/http"
)

func HealthCheck(c *gin.Context) {

	App := c.MustGet("App")
	db := App.(*app.App).DB

	err := db.Ping()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "OK",
	})
}

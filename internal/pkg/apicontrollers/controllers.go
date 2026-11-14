package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"github.com/gin-gonic/gin"
	"net/http"
)

func HealthCheck(c *gin.Context) {

	App := c.MustGet("App")
	db := App.(*app.App).DB

	// open to anyone: the reason is only logged, it tells the database's address
	err := db.Ping()
	if err != nil {
		App.(*app.App).Logger.Error().Err(err).Msg("health check: the database doesn't answer")
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "OK",
	})
}

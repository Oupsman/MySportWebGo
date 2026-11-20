package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/vars"
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

	// the version: which release answers, after an upgrade
	c.JSON(http.StatusOK, gin.H{
		"status":  "OK",
		"version": vars.Version,
	})
}

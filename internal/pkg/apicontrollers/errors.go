package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/services/activityService"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The errors of the database and of the files tell the server's internals (SQL, columns, paths): they
// are logged, the client only learns that the request failed.

// internalError logs err and answers 500 without it
func internalError(c *gin.Context, err error) {
	if App, ok := c.Get("App"); ok {
		App.(*app.App).Logger.Error().Err(err).Str("method", c.Request.Method).Str("route", c.FullPath()).
			Msg("request failed")
	}
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error, see the server logs"})
}

// lookupError answers 404 "<what> not found" when err is gorm.ErrRecordNotFound, else 500 as internalError
func lookupError(c *gin.Context, err error, what string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": what + " not found"})
		return
	}
	internalError(c, err)
}

// badRequest answers 400 with err: for the errors about the request itself (JSON, parameters), which
// tell the client what to fix. A body above its size limit gets 413.
func badRequest(c *gin.Context, err error) {
	if middlewares.RequestTooLarge(c, err) {
		return
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

// analysisError answers the failure of analysing (or trimming) a FIT file: 503 when the server is busy,
// 400 with the reason for a file that isn't a valid activity or a refused trim, 500 for the others (the
// file can't be read, the database fails)
func analysisError(c *gin.Context, err error) {
	var invalid *activityService.InvalidFitError
	switch {
	case serverBusy(c, err):
	case errors.As(err, &invalid), errors.Is(err, activityService.ErrTrimTooShort),
		errors.Is(err, activityService.ErrNoFitFile):
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		internalError(c, err)
	}
}

// serverBusy answers 503 and returns true when err is activityService.ErrBusy: the server has too much
// heavy work queued, the client can send the request again later
func serverBusy(c *gin.Context, err error) bool {
	if !errors.Is(err, activityService.ErrBusy) {
		return false
	}
	c.Header("Retry-After", "30")
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	return true
}

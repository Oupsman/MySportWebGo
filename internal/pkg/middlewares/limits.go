package middlewares

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// LimitBody caps the size of the request bodies: limits gives the one of some routes, by their path as
// registered ("/api/v1/activity"), the others get defaultLimit. Reading past it fails with an
// *http.MaxBytesError and the connection is closed: a client can't make the server read nor store more.
func LimitBody(defaultLimit int64, limits map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, ok := limits[c.FullPath()]
		if !ok {
			limit = defaultLimit
		}
		// announced too large: refused before reading anything
		if c.Request.ContentLength > limit {
			abortTooLarge(c, limit)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// RequestTooLarge answers 413 and returns true when err comes from a body larger than its LimitBody limit.
// The handlers call it on the errors of reading the body, before answering 400.
func RequestTooLarge(c *gin.Context, err error) bool {
	var maxBytes *http.MaxBytesError
	if !errors.As(err, &maxBytes) {
		return false
	}
	abortTooLarge(c, maxBytes.Limit)
	return true
}

func abortTooLarge(c *gin.Context, limit int64) {
	c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge,
		gin.H{"error": "the request is too large, the limit is " + strconv.FormatInt(limit>>20, 10) + " MB"})
}

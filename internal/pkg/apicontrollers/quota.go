package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/services/activityService"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// QuotaExceeded is the code of the answer refusing a file above the storage quota of the user
const QuotaExceeded = "quota_exceeded"

// formatBytes writes a size as people read it: 512 KB, 3.4 MB, 1.00 GB
func formatBytes(bytes int64) string {
	switch {
	case bytes < 1<<20:
		return fmt.Sprintf("%d KB", (bytes+512)>>10)
	case bytes < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	default:
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1<<30))
	}
}

// withinQuota tells whether the user may store incoming more bytes: their files and incoming within their
// quota. False, with 507 answered, otherwise. The file stored may end a bit larger than incoming (its
// thumbnail, the GPX it was converted from): the quota is exceeded by one file at most.
func withinQuota(c *gin.Context, user models.Users, incoming int64) bool {
	quota := user.Quota()
	if quota == 0 {
		return true
	}
	used := diskUsage(activityService.UserMediaDir(user.UUID))
	if used+incoming <= quota {
		return true
	}
	c.AbortWithStatusJSON(http.StatusInsufficientStorage, gin.H{"code": QuotaExceeded, "used": used, "quota": quota,
		"error": fmt.Sprintf("your storage is full: %s used of %s. Delete activities, or ask an admin for more space",
			formatBytes(used), formatBytes(quota))})
	return false
}

// GetStorage answers the space the files of the user take, and their quota (0: none)
func GetStorage(c *gin.Context) {
	user := middlewares.CurrentUser(c)
	c.JSON(http.StatusOK, gin.H{"used": diskUsage(activityService.UserMediaDir(user.UUID)), "quota": user.Quota()})
}

// SetUserQuota gives a user their own storage quota, {"quota_bytes": bytes} (0 for none), or the one of the
// instance again, {"quota_bytes": null}; with a code of the authenticator. An admin may change their own.
func SetUserQuota(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	var body map[string]json.RawMessage
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	raw, ok := body["quota_bytes"]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quota_bytes is missing: bytes, 0 for no quota, null for the one of the instance"})
		return
	}
	var quota *int64
	if err := json.Unmarshal(raw, &quota); err != nil || (quota != nil && *quota < 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quota_bytes must be a number of bytes, 0 for no quota, or null"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	user, err := App.DB.GetUser(id)
	if err != nil {
		lookupError(c, err, "user")
		return
	}
	if !RequireTOTP(c) {
		return
	}
	if err := App.DB.SetUserQuota(user.ID, quota); err != nil {
		lookupError(c, err, "user")
		return
	}
	user.QuotaBytes = quota
	log := App.Logger.Info().Uint("user_id", user.ID).Uint("by", middlewares.CurrentUser(c).ID)
	if quota != nil {
		log = log.Int64("quota_bytes", *quota)
	}
	log.Msg("storage quota changed")
	detail := "the quota of the instance"
	switch {
	case quota == nil:
	case *quota == 0:
		detail = "no quota"
	default:
		detail = strconv.FormatInt(*quota, 10) + " bytes"
	}
	recordAdminEvent(c, user, models.EventAdminQuota, detail)
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "quota_bytes": quota, "quota": user.Quota()})
}

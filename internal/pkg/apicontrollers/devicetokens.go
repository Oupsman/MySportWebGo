package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"time"
)

const (
	maxDeviceTokens        = 20
	maxDeviceTokenLifetime = 366 * 24 * time.Hour
)

// CreateDeviceToken creates a token for a device or a script, its value is only returned here
func CreateDeviceToken(c *gin.Context) {
	var body struct {
		Name        string            `json:"name" binding:"required,max=100"`
		Permissions models.Permission `json:"permissions"`
		ExpiresAt   time.Time         `json:"expires_at" binding:"required"`
	}
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Permissions == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "a device token needs at least one permission"})
		return
	}
	now := time.Now()
	if !body.ExpiresAt.After(now) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "expires_at must be in the future"})
		return
	}
	if body.ExpiresAt.After(now.Add(maxDeviceTokenLifetime)) {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "a device token expires within a year"})
		return
	}
	count, err := db.CountDeviceTokens(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if count >= maxDeviceTokens {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "too many device tokens, revoke one first"})
		return
	}

	token, hash := utils.NewDeviceToken()
	deviceToken := models.DeviceTokens{
		UserID:      user.ID,
		Name:        body.Name,
		Hash:        hash,
		Hint:        token[:len(utils.DeviceTokenPrefix)+4],
		Permissions: body.Permissions,
		ExpiresAt:   body.ExpiresAt,
	}
	if err := db.CreateDeviceToken(&deviceToken); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"token": token, "device_token": deviceToken})
}

func GetDeviceTokens(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	tokens, err := db.GetDeviceTokens(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"device_tokens": tokens})
}

func DeleteDeviceToken(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	deleted, err := db.DeleteDeviceToken(user.ID, id)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !deleted {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "device token not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "device token revoked"})
}

package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

// DeviceUpdate is what the user changes on a device
type DeviceUpdate struct {
	AltitudeCorrection string `json:"altitude_correction" binding:"required"`
}

// GetDevices returns the recording devices of the user, with their activities, the latest used first
func GetDevices(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	devices, err := db.GetDevicesStats(user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, devices)
}

// GetDevice returns a recording device of the user; 404 whether it doesn't exist or isn't theirs
func GetDevice(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	id, err := utils.GetUUID(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	device, err := db.GetDeviceStats(user.ID, id)
	if err != nil {
		lookupError(c, err, "device")
		return
	}
	c.JSON(http.StatusOK, device)
}

// UpdateDevice changes the altitude correction of a recording device of the user. It applies to the
// activities imported after; the ones recorded before are recomputed on request.
func UpdateDevice(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	id, err := utils.GetUUID(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	var update DeviceUpdate
	if err := c.ShouldBindJSON(&update); err != nil {
		badRequest(c, err)
		return
	}
	if !models.ValidAltitudeCorrection(update.AltitudeCorrection) {
		c.AbortWithStatusJSON(http.StatusBadRequest,
			gin.H{"error": "altitude_correction must be none, replace or calibrate"})
		return
	}
	if err := db.SetAltitudeCorrection(user.ID, id, update.AltitudeCorrection); err != nil {
		lookupError(c, err, "device")
		return
	}
	device, err := db.GetDeviceStats(user.ID, id)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, device)
}

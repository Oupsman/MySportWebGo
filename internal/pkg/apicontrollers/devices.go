package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"errors"
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
	for i := range devices {
		devices[i].Recompute = activityService.RecomputeStatusOf(devices[i].ID)
	}
	c.JSON(http.StatusOK, devices)
}

// withRecompute is a device with the progress of its latest recompute
func withRecompute(device models.DeviceStats) models.DeviceStats {
	device.Recompute = activityService.RecomputeStatusOf(device.ID)
	return device
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
	c.JSON(http.StatusOK, withRecompute(device))
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
	c.JSON(http.StatusOK, withRecompute(device))
}

// DeviceMerge names the device another one is merged into
type DeviceMerge struct {
	Into string `json:"into" binding:"required"`
}

// MergeDevice merges a recording device of the user into another one: the same unit, that its files named
// differently. It answers the device merged into, with its activities.
func MergeDevice(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	id, err := utils.GetUUID(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	var merge DeviceMerge
	if err := c.ShouldBindJSON(&merge); err != nil {
		badRequest(c, err)
		return
	}
	into, err := utils.GetUUID(merge.Into)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "into must be the id of a device"})
		return
	}
	if err := db.MergeDevices(user.ID, id, into); err != nil {
		if errors.Is(err, models.ErrMergeDevice) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		lookupError(c, err, "device")
		return
	}
	device, err := db.GetDeviceStats(user.ID, into)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, withRecompute(device))
}

// RecomputeDevice analyses again, in the background, the activities of a recording device of the user with
// its altitude correction. It answers 202 and the device, whose recompute tells the progress; 409 when its
// activities are being recomputed.
func RecomputeDevice(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	user := middlewares.CurrentUser(c)
	id, err := utils.GetUUID(c.Param("id"))
	if err != nil {
		badRequest(c, err)
		return
	}
	// a device of the user, not merged into another
	if _, err := db.GetDeviceStats(user.ID, id); err != nil {
		lookupError(c, err, "device")
		return
	}
	if _, err := activityService.StartRecompute(&db, user.ID, id, App.Logger); err != nil {
		if errors.Is(err, activityService.ErrRecomputeRunning) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		internalError(c, err)
		return
	}
	device, err := db.GetDeviceStats(user.ID, id)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, withRecompute(device))
}

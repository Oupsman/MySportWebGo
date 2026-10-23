package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/pictures"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

// equipmentOwner returns the user of the request, and the equipment of the :id parameter when it belongs to them.
// It aborts the request on error.
func equipmentOwner(c *gin.Context) (models.Users, models.Equipments, bool) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	equipmentUUID, err := utils.GetUUID(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return models.Users{}, models.Equipments{}, false
	}
	equipment, err := db.GetEquipment(equipmentUUID)
	if err != nil || equipment.UserID != int(user.ID) {
		// same answer whether the equipment exists or not
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "equipment not found"})
		return models.Users{}, models.Equipments{}, false
	}
	return user, equipment, true
}

func CreateEquipment(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	var equipment models.Equipments
	err := c.ShouldBindJSON(&equipment)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// set after binding, so the request body can't choose them; the picture has its own upload
	equipment.ID = uuid.UUID{}
	equipment.UserID = int(user.ID)
	equipment.Picture = ""

	err = db.CreateEquipment(&equipment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, equipment)
}

func GetDefaultEquipment(c *gin.Context) {

	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	equipment := db.GetDefaultEquipment(user.ID)

	c.JSON(http.StatusOK, equipment)

}

func UpdateEquipment(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}

	var update models.Equipments
	err := c.ShouldBindJSON(&update)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// only the user editable fields, ownership and id come from the database
	equipment.Name = update.Name
	equipment.Brand = update.Brand
	equipment.EquipmentModel = update.EquipmentModel
	equipment.DateOfPurchase = update.DateOfPurchase
	equipment.InitialMileage = update.InitialMileage
	equipment.Weight = update.Weight
	equipment.MaintenanceInterval = update.MaintenanceInterval
	equipment.IsDefault = update.IsDefault
	equipment.IsCommute = update.IsCommute

	err = db.UpdateEquipment(&equipment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, equipment)
}

// AssignCommuteEquipment gives the commute equipment to the commute rides recorded before it was chosen
func AssignCommuteEquipment(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	if !equipment.IsCommute {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "this equipment is not the commute one"})
		return
	}
	updated, err := db.AssignCommuteEquipment(user.ID, equipment.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

func GetEquipment(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	stats, err := db.GetEquipmentStats(user.ID, equipment.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func GetEquipments(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)

	equipments, err := db.GetEquipmentsStats(user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, equipments)
}

func DeleteEquipment(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	user, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}

	err := db.DeleteEquipment(equipment.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// the equipment is gone, its pictures only waste space
	if err := activityService.RemoveEquipmentPictures(user.UUID, equipment.ID); err != nil {
		App.Logger.Warn().Err(err).Str("equipment", equipment.ID.String()).Msg("pictures not removed")
	}

	c.JSON(http.StatusOK, gin.H{"success": "equipment deleted"})
}

// UploadEquipmentPicture sets the picture of an equipment from the "file" of a multipart form,
// replacing the previous one
func UploadEquipmentPicture(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	user, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	header, err := c.FormFile("file")
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}
	file, err := header.Open()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer file.Close()
	picture, err := pictures.Process(file)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	path, err := activityService.SaveEquipmentPicture(user.UUID, equipment.ID, picture)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := db.SetEquipmentPicture(equipment.ID, path); err != nil {
		_ = activityService.RemoveEquipmentPicture(user.UUID, path)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := activityService.RemoveEquipmentPicture(user.UUID, equipment.Picture); err != nil {
		App.Logger.Warn().Err(err).Str("equipment", equipment.ID.String()).Msg("previous picture not removed")
	}
	c.JSON(http.StatusOK, gin.H{"picture": path})
}

func DeleteEquipmentPicture(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := App.DB
	user, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	if err := db.SetEquipmentPicture(equipment.ID, ""); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := activityService.RemoveEquipmentPicture(user.UUID, equipment.Picture); err != nil {
		App.Logger.Warn().Err(err).Str("equipment", equipment.ID.String()).Msg("picture not removed")
	}
	c.JSON(http.StatusOK, gin.H{"success": "picture removed"})
}

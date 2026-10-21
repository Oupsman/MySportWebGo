package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

// equipmentOwner returns the user of the request, and the equipment of the :id parameter when it belongs to them.
// It aborts the request on error.
func equipmentOwner(c *gin.Context) (models.Users, models.Equipments, bool) {
	db := c.MustGet("App").(*app.App).DB
	userUUID, err := utils.GetUserUUID(c.Request.Header.Get("Authorization"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return models.Users{}, models.Equipments{}, false
	}
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return models.Users{}, models.Equipments{}, false
	}
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
	App := c.MustGet("App").(*app.App)
	db := App.DB
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var equipment models.Equipments
	err = c.ShouldBindJSON(&equipment)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// set after binding, so the request body can't choose them
	equipment.ID = uuid.UUID{}
	equipment.UserID = int(user.ID)

	err = db.CreateEquipment(&equipment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, equipment)
}

func GetDefaultEquipment(c *gin.Context) {

	app := c.MustGet("App").(*app.App)
	db := app.DB
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

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

	err = db.UpdateEquipment(&equipment)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, equipment)
}

func GetEquipment(c *gin.Context) {
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, equipment)
}

func GetEquipments(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	db := App.DB
	user, err := db.GetUserByUUID(userUUID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	equipments := db.GetEquipments(user.ID)
	if equipments == nil {
		equipments = []models.Equipments{}
	}

	c.JSON(http.StatusOK, equipments)
}

func DeleteEquipment(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}

	err := db.DeleteEquipment(equipment.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": "equipment deleted"})
}

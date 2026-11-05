package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// serviceInput is what the user can set on a service, the equipment comes from the URL
type serviceInput struct {
	Date        string `json:"date"`
	Mileage     int    `json:"mileage" binding:"min=0"`
	Description string `json:"description"`
}

// apply copies the input to service, the date defaults to now
func (in serviceInput) apply(service *models.Services) error {
	date, err := parseServiceDate(in.Date)
	if err != nil {
		return err
	}
	service.Date = date
	service.Mileage = in.Mileage
	service.Description = in.Description
	return nil
}

func parseServiceDate(value string) (date time.Time, err error) {
	if value == "" {
		return time.Now(), nil
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if date, err = time.Parse(layout, value); err == nil {
			return date, nil
		}
	}
	return time.Time{}, errors.New("invalid date, expected YYYY-MM-DD or RFC 3339")
}

// equipmentService returns the equipment of the :id parameter when it belongs to the user of the request,
// and its service of the :service parameter. It aborts the request on error.
func equipmentService(c *gin.Context) (models.Services, bool) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return models.Services{}, false
	}
	serviceID, err := utils.GetUUID(c.Param("service"))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return models.Services{}, false
	}
	service, err := db.GetService(equipment.ID, serviceID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return models.Services{}, false
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return models.Services{}, false
	}
	return service, true
}

func CreateService(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}

	var input serviceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service := models.Services{ID: uuid.Nil, EquipmentID: equipment.ID}
	if err := input.apply(&service); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := db.CreateService(&service); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, service)
}

func GetServices(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	services, err := db.GetServices(equipment.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, services)
}

func GetService(c *gin.Context) {
	service, ok := equipmentService(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, service)
}

func UpdateService(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	service, ok := equipmentService(c)
	if !ok {
		return
	}

	var input serviceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := input.apply(&service); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := db.UpdateService(&service); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, service)
}

func DeleteService(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	service, ok := equipmentService(c)
	if !ok {
		return
	}
	if err := db.DeleteService(service.EquipmentID, service.ID); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "service deleted"})
}

package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// maxReminderName is the longest name of a reminder, in characters
const maxReminderName = 100

// reminderInput is what the user sets of a reminder, the equipment comes from the URL
type reminderInput struct {
	Name       string `json:"name"`
	EveryKm    int    `json:"every_km"`
	EveryHours int    `json:"every_hours"`
	EveryCount int    `json:"every_count"`
	EveryUnit  string `json:"every_unit"`
	// OnlyWhenUsed: true when not sent
	OnlyWhenUsed *bool `json:"only_when_used"`
	// StartsAt starts its cycle until it is done: now when not sent on a creation, kept on an update
	StartsAt string `json:"starts_at"`
}

// apply checks the input and copies it to reminder
func (in reminderInput) apply(reminder *models.MaintenanceReminder) error {
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "":
		return errors.New("a reminder needs a name")
	case len([]rune(name)) > maxReminderName:
		return errors.New("the name of a reminder is 100 characters at most")
	case in.EveryKm < 0 || in.EveryHours < 0 || in.EveryCount < 0:
		return errors.New("an interval can't be negative")
	case in.EveryCount > 0 && !slices.Contains(models.ReminderUnits, in.EveryUnit):
		return errors.New("every_unit must be day, week, month or year")
	case in.EveryKm == 0 && in.EveryHours == 0 && in.EveryCount == 0:
		return errors.New("a reminder needs an interval: km, hours, or time")
	}
	reminder.Name = name
	reminder.EveryKm = in.EveryKm
	reminder.EveryHours = in.EveryHours
	reminder.EveryCount = in.EveryCount
	reminder.EveryUnit = in.EveryUnit
	if in.EveryCount == 0 {
		reminder.EveryUnit = ""
	}
	reminder.OnlyWhenUsed = in.OnlyWhenUsed == nil || *in.OnlyWhenUsed
	if in.StartsAt != "" || reminder.StartsAt.IsZero() {
		starts, err := parseServiceDate(in.StartsAt)
		if err != nil {
			return err
		}
		reminder.StartsAt = starts
	}
	return nil
}

// equipmentReminder returns the reminder of the :reminder parameter of the equipment of the :id parameter,
// of the user of the request. It aborts the request on error.
func equipmentReminder(c *gin.Context) (models.Equipments, models.MaintenanceReminder, bool) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return equipment, models.MaintenanceReminder{}, false
	}
	id, err := utils.GetUUID(c.Param("reminder"))
	if err != nil {
		badRequest(c, err)
		return equipment, models.MaintenanceReminder{}, false
	}
	reminder, err := db.GetReminder(equipment.ID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "reminder not found"})
		return equipment, reminder, false
	}
	if err != nil {
		internalError(c, err)
		return equipment, reminder, false
	}
	return equipment, reminder, true
}

// answerReminder answers a reminder with its cycle
func answerReminder(c *gin.Context, status int, reminder models.MaintenanceReminder) {
	db := c.MustGet("App").(*app.App).DB
	s, err := db.GetReminderStatus(reminder.EquipmentID, reminder.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(status, s)
}

// GetReminders answers the reminders of an equipment with their cycle, the most urgent first
func GetReminders(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	reminders, err := db.GetEquipmentReminders(equipment.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, reminders)
}

func CreateReminder(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, equipment, ok := equipmentOwner(c)
	if !ok {
		return
	}
	var input reminderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, err)
		return
	}
	reminder := models.MaintenanceReminder{EquipmentID: equipment.ID}
	if err := input.apply(&reminder); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.CreateReminder(&reminder); err != nil {
		internalError(c, err)
		return
	}
	answerReminder(c, http.StatusCreated, reminder)
}

func UpdateReminder(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, reminder, ok := equipmentReminder(c)
	if !ok {
		return
	}
	var input reminderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, err)
		return
	}
	if err := input.apply(&reminder); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.UpdateReminder(&reminder); err != nil {
		internalError(c, err)
		return
	}
	answerReminder(c, http.StatusOK, reminder)
}

// DeleteReminder deletes a reminder, the services done for it are kept
func DeleteReminder(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	_, reminder, ok := equipmentReminder(c)
	if !ok {
		return
	}
	if err := db.DeleteReminder(reminder.EquipmentID, reminder.ID); err != nil {
		lookupError(c, err, "reminder")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": "reminder deleted"})
}

// ReminderDone records a reminder done: a service of the equipment done for it, {date, mileage,
// description} all optional (now, the mileage of the equipment, the name of the reminder). It answers the
// service and the reminder with its new cycle.
func ReminderDone(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	equipment, reminder, ok := equipmentReminder(c)
	if !ok {
		return
	}
	var body struct {
		Date        string `json:"date"`
		Mileage     *int   `json:"mileage"`
		Description string `json:"description"`
	}
	// an empty body is everything by default
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			badRequest(c, err)
			return
		}
	}
	date, err := parseServiceDate(body.Date)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	service := models.Services{EquipmentID: equipment.ID, Date: date, Description: strings.TrimSpace(body.Description),
		ReminderIDs: []uuid.UUID{reminder.ID}}
	if service.Description == "" {
		service.Description = reminder.Name
	}
	if body.Mileage != nil {
		if *body.Mileage < 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "the mileage can't be negative"})
			return
		}
		service.Mileage = *body.Mileage
	} else {
		stats, err := db.GetEquipmentStats(user.ID, equipment.ID)
		if err != nil {
			internalError(c, err)
			return
		}
		service.Mileage = stats.Mileage
	}
	if err := db.CreateService(&service); err != nil {
		serviceError(c, err)
		return
	}
	status, err := db.GetReminderStatus(equipment.ID, reminder.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"service": service, "reminder": status})
}

// UserMaintenance answers the reminders of all the equipments of the user due or soon, the most urgent
// first, for the dashboard; ?all=true answers them all
func UserMaintenance(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user := middlewares.CurrentUser(c)
	reminders, err := db.GetUserReminders(user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	if c.Query("all") != "true" {
		reminders = slices.DeleteFunc(reminders, func(r models.ReminderStatus) bool { return r.State == models.ReminderOK })
	}
	c.JSON(http.StatusOK, gin.H{"reminders": reminders, "checked_at": time.Now()})
}

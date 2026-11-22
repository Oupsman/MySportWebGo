package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Services is a maintenance done on an equipment
type Services struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `sql:"index" json:"-"`
	Equipment   Equipments `json:"-" gorm:"constraint:OnDelete:CASCADE"`
	EquipmentID uuid.UUID  `json:"equipment_id" gorm:"type:uuid;not null;index"`
	Date        time.Time  `json:"date"`
	Mileage     int        `json:"mileage"` // km of the equipment when serviced
	Description string     `json:"description"`
	// ReminderIDs are the maintenance reminders it did: their next cycle starts at its date
	ReminderIDs []uuid.UUID `json:"reminder_ids" gorm:"-"`
}

// CreateService creates a service with the reminders it did, ErrUnknownReminder for one of another equipment
func (db *DB) CreateService(service *Services) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Equipment").Create(service).Error; err != nil {
			return err
		}
		return setServiceReminders(tx, service)
	})
}

// GetServices returns the services of an equipment, the latest first
func (db *DB) GetServices(equipmentID uuid.UUID) ([]Services, error) {
	services := []Services{}
	err := db.Where("equipment_id = ?", equipmentID).Order("date DESC, created_at DESC").Find(&services).Error
	if err != nil {
		return services, err
	}
	return services, db.withReminderIDs(services)
}

// GetService returns a service of an equipment, gorm.ErrRecordNotFound when the equipment doesn't have it
func (db *DB) GetService(equipmentID, serviceID uuid.UUID) (Services, error) {
	var service Services
	err := db.Where("id = ? AND equipment_id = ?", serviceID, equipmentID).First(&service).Error
	if err != nil {
		return service, err
	}
	list := []Services{service}
	err = db.withReminderIDs(list)
	return list[0], err
}

// UpdateService saves a service and the reminders it did (ReminderIDs), ErrUnknownReminder for one of another
// equipment
func (db *DB) UpdateService(service *Services) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Equipment").Save(service).Error; err != nil {
			return err
		}
		return setServiceReminders(tx, service)
	})
}

// DeleteService deletes a service of an equipment, gorm.ErrRecordNotFound when the equipment doesn't have it
func (db *DB) DeleteService(equipmentID, serviceID uuid.UUID) error {
	result := db.Where("id = ? AND equipment_id = ?", serviceID, equipmentID).Delete(&Services{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

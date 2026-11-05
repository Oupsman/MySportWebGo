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
}

func (db *DB) CreateService(service *Services) error {
	return db.Omit("Equipment").Create(service).Error
}

// GetServices returns the services of an equipment, the latest first
func (db *DB) GetServices(equipmentID uuid.UUID) ([]Services, error) {
	services := []Services{}
	err := db.Where("equipment_id = ?", equipmentID).Order("date DESC, created_at DESC").Find(&services).Error
	return services, err
}

// GetService returns a service of an equipment, gorm.ErrRecordNotFound when the equipment doesn't have it
func (db *DB) GetService(equipmentID, serviceID uuid.UUID) (Services, error) {
	var service Services
	err := db.Where("id = ? AND equipment_id = ?", serviceID, equipmentID).First(&service).Error
	return service, err
}

func (db *DB) UpdateService(service *Services) error {
	return db.Omit("Equipment").Save(service).Error
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

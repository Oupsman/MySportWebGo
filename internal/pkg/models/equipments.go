package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

// /MEDIA/user uuid/equipments/equipment uuid/pic filename.pic extension

type Equipments struct {
	ID                  uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DeletedAt           *time.Time `sql:"index" json:"-"`
	Name                string     `json:"name"`
	Brand               string     `json:"brand"`
	EquipmentModel      string     `json:"model"`
	DateOfPurchase      time.Time  `json:"date_of_purchase"`
	InitialMileage      int        `json:"initial_mileage"`
	Mileage             int        `json:"mileage"`
	Weight              int        `json:"weight"`
	User                Users      `json:"-"`
	UserID              int        `json:"user_id"`
	MaintenanceInterval int        `json:"maintenance_interval"`
	IsDefault           bool       `json:"is_default"`
}

// CreateEquipment saves a new equipment, it becomes the only default one of the user when IsDefault is set
func (db *DB) CreateEquipment(equipment *Equipments) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if equipment.IsDefault {
			err := tx.Model(&Equipments{}).Where("user_id = ?", equipment.UserID).Update("is_default", false).Error
			if err != nil {
				return err
			}
		}
		return tx.Omit("User").Create(equipment).Error
	})
}

func (db *DB) GetDefaultEquipment(userID uint) Equipments {
	var equipment Equipments
	result := db.Where("user_id = ? AND is_default = ?", userID, true).First(&equipment)
	if result.Error != nil {
		return Equipments{}
	}
	return equipment
}

// UpdateEquipment saves an equipment, it becomes the only default one of the user when IsDefault is set
func (db *DB) UpdateEquipment(equipment *Equipments) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if equipment.IsDefault {
			err := tx.Model(&Equipments{}).Where("user_id = ? AND id <> ?", equipment.UserID, equipment.ID).
				Update("is_default", false).Error
			if err != nil {
				return err
			}
		}
		return tx.Omit("User").Save(equipment).Error
	})
}

func (db *DB) GetEquipment(uuid uuid.UUID) (Equipments, error) {
	var equipment Equipments
	err := db.Where("id = ?", uuid).First(&equipment).Error
	if err != nil {
		return equipment, err
	}
	return equipment, nil

}

func (db *DB) GetEquipments(id uint) []Equipments {
	var equipments []Equipments
	db.Where("user_id = ?", id).Find(&equipments)
	return equipments
}

// DeleteEquipment deletes an equipment, its activities are kept without equipment
func (db *DB) DeleteEquipment(uuid uuid.UUID) error {
	return db.Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Activity{}).Where("equipment_id = ?", uuid).Update("equipment_id", nil).Error
		if err != nil {
			return err
		}
		return tx.Where("id = ?", uuid).Delete(&Equipments{}).Error
	})
}

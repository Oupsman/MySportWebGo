package models

import (
	"github.com/google/uuid"
	"time"
)

// /MEDIA/user uuid/equipments/equipment uuid/pic filename.pic extension

type Equipments struct {
	ID                  uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time `sql:"index"`
	Name                string     `json:"name"`
	Brand               string     `json:"brand"`
	EquipmentModel      string     `json:"model"`
	DateOfPurchase      time.Time  `json:"date_of_purchase"`
	InitialMileage      int        `json:"initial_mileage"`
	Mileage             int        `json:"mileage"`
	Weight              int        `json:"weight"`
	User                Users
	UserID              int
	MaintenanceInterval int  `json:"maintenance_interval"`
	IsDefault           bool `json:"is_default"`
}

func (db *DB) CreateEquipment(equipment Equipments) error {
	result := db.Create(&equipment)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (db *DB) GetDefaultEquipment(userID uint) Equipments {
	var equipment Equipments
	result := db.Where("user_id = ? AND is_default = ?", userID, true).First(&equipment)
	if result.Error != nil {
		return Equipments{}
	}
	return equipment
}

package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"math"
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

// EquipmentStats is an equipment with the totals of its activities
type EquipmentStats struct {
	Equipments
	Activities int64      `json:"activities"`
	Distance   float64    `json:"distance"` // meters, of the activities only
	Duration   int64      `json:"duration"` // seconds
	LastUsed   *time.Time `json:"last_used"`
}

// GetEquipmentsStats returns the equipments of a user with their totals, the default one first.
// The mileage is computed: the initial one plus the distance of the activities, in km.
func (db *DB) GetEquipmentsStats(userID uint) ([]EquipmentStats, error) {
	return db.equipmentsStats("e.user_id = ?", userID)
}

func (db *DB) GetEquipmentStats(userID uint, id uuid.UUID) (EquipmentStats, error) {
	stats, err := db.equipmentsStats("e.user_id = ? AND e.id = ?", userID, id)
	if err != nil {
		return EquipmentStats{}, err
	}
	if len(stats) == 0 {
		return EquipmentStats{}, gorm.ErrRecordNotFound
	}
	return stats[0], nil
}

func (db *DB) equipmentsStats(where string, args ...interface{}) ([]EquipmentStats, error) {
	stats := []EquipmentStats{}
	err := db.Raw("SELECT e.*, COUNT(a.id) AS activities, COALESCE(SUM(a.distance), 0) AS distance, "+
		"COALESCE(SUM(a.duration), 0) AS duration, MAX(a.date) AS last_used "+
		"FROM equipments e LEFT JOIN activities a ON a.equipment_id = e.id "+
		"WHERE "+where+" GROUP BY e.id ORDER BY e.is_default DESC, e.name", args...).Scan(&stats).Error
	for i := range stats {
		stats[i].Mileage = stats[i].InitialMileage + int(math.Round(stats[i].Distance/1000))
	}
	return stats, err
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

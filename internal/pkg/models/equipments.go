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
	IsCommute           bool       `json:"is_commute"` // given to the commute rides, one per user
	Picture             string     `json:"picture"`    // URL path of its picture under /MEDIA, empty without
}

// CommuteSport is the sport of the activities getting the commute equipment
const CommuteSport = "cycling"

// keepOnlyFlag clears flag (is_default, is_commute) on the other equipments of the user of equipment
func keepOnlyFlag(tx *gorm.DB, equipment *Equipments, flag string) error {
	query := tx.Model(&Equipments{}).Where("user_id = ?", equipment.UserID)
	if equipment.ID != uuid.Nil {
		query = query.Where("id <> ?", equipment.ID)
	}
	return query.Update(flag, false).Error
}

// CreateEquipment saves a new equipment, it becomes the only default one of the user when IsDefault is set,
// and the only commute one when IsCommute is set
func (db *DB) CreateEquipment(equipment *Equipments) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := keepOnlyFlags(tx, equipment); err != nil {
			return err
		}
		return tx.Omit("User").Create(equipment).Error
	})
}

func keepOnlyFlags(tx *gorm.DB, equipment *Equipments) error {
	if equipment.IsDefault {
		if err := keepOnlyFlag(tx, equipment, "is_default"); err != nil {
			return err
		}
	}
	if equipment.IsCommute {
		return keepOnlyFlag(tx, equipment, "is_commute")
	}
	return nil
}

// GetCommuteEquipment returns the equipment of the user's commutes, nil when there is none
func (db *DB) GetCommuteEquipment(userID uint) (*Equipments, error) {
	var equipments []Equipments
	err := db.Where("user_id = ? AND is_commute", userID).Limit(1).Find(&equipments).Error
	if err != nil || len(equipments) == 0 {
		return nil, err
	}
	return &equipments[0], nil
}

// AssignCommuteEquipment gives an equipment to the user's commute rides, it returns how many changed
func (db *DB) AssignCommuteEquipment(userID uint, equipmentID uuid.UUID) (int64, error) {
	result := db.Model(&Activity{}).
		Where("user_id = ? AND is_commute AND sport = ? AND (equipment_id IS NULL OR equipment_id <> ?)",
			userID, CommuteSport, equipmentID).
		Update("equipment_id", equipmentID)
	return result.RowsAffected, result.Error
}

func (db *DB) GetDefaultEquipment(userID uint) Equipments {
	var equipment Equipments
	result := db.Where("user_id = ? AND is_default = ?", userID, true).First(&equipment)
	if result.Error != nil {
		return Equipments{}
	}
	return equipment
}

// UpdateEquipment saves an equipment, it becomes the only default one of the user when IsDefault is set,
// and the only commute one when IsCommute is set
func (db *DB) UpdateEquipment(equipment *Equipments) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := keepOnlyFlags(tx, equipment); err != nil {
			return err
		}
		return tx.Omit("User").Save(equipment).Error
	})
}

// SetEquipmentPicture saves the URL path of the picture of an equipment, empty to remove it
func (db *DB) SetEquipmentPicture(id uuid.UUID, picture string) error {
	return db.Model(&Equipments{}).Where("id = ?", id).Update("picture", picture).Error
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

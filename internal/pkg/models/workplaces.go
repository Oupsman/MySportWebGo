package models

import (
	"MySportWeb/internal/pkg/types"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Workplace is a place where a user works: a circle of Diameter meters around Lat, Lon. An activity
// starting or ending in it is a commute.
type Workplace struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UserID    uint      `gorm:"not null" json:"-"`
	Name      string    `json:"name"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Diameter  float64   `json:"diameter"` // meters
}

// GetWorkplaces returns the workplaces of a user, oldest first
func (db *DB) GetWorkplaces(userID uint) ([]Workplace, error) {
	workplaces := []Workplace{}
	err := db.Where("user_id = ?", userID).Order("created_at, id").Find(&workplaces).Error
	return workplaces, err
}

// GetWorkplace returns a workplace of a user, gorm.ErrRecordNotFound when they have no such workplace
func (db *DB) GetWorkplace(userID uint, id uuid.UUID) (Workplace, error) {
	var workplace Workplace
	err := db.Where("id = ? AND user_id = ?", id, userID).First(&workplace).Error
	return workplace, err
}

// SaveWorkplace creates or updates a workplace
func (db *DB) SaveWorkplace(w *Workplace) error {
	if w.ID == uuid.Nil {
		return db.Create(w).Error
	}
	return db.Save(w).Error
}

// DeleteWorkplace deletes a workplace of a user; its commutes stay commutes
func (db *DB) DeleteWorkplace(userID uint, id uuid.UUID) error {
	result := db.Where("id = ? AND user_id = ?", id, userID).Delete(&Workplace{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CommuteCandidate is what deciding if an activity is a commute reads of it
type CommuteCandidate struct {
	ID            uuid.UUID
	Date          time.Time
	Sport         string
	StartPosition *types.GpsPoint
	EndPosition   *types.GpsPoint
}

// CommuteCandidates returns the activities of a user not commutes, with a start or an end position
func (db *DB) CommuteCandidates(userID uint) ([]CommuteCandidate, error) {
	var candidates []CommuteCandidate
	err := db.Model(&Activity{}).Select("id, date, sport, start_position, end_position").
		Where("user_id = ? AND NOT is_commute AND (start_position IS NOT NULL OR end_position IS NOT NULL)", userID).
		Order("date").Scan(&candidates).Error
	return candidates, err
}

// MarkCommutes makes the activities ids of a user commutes, with the CO2 they saved; the rides among them
// get the commute bike of the user, when they were recorded since its purchase
func (db *DB) MarkCommutes(userID uint, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Activity{}).Where("user_id = ? AND id IN ?", userID, ids).Updates(map[string]interface{}{
			"is_commute": true,
			"co2":        gorm.Expr("CASE WHEN co2 > 0 THEN co2 ELSE distance * ? END", Co2PerMeter),
		}).Error
		if err != nil {
			return err
		}
		bike, err := (&DB{DB: *tx}).GetCommuteEquipment(userID)
		if err != nil || bike == nil {
			return err
		}
		return tx.Model(&Activity{}).Where("user_id = ? AND id IN ? AND sport = ? AND date >= ?",
			userID, ids, CommuteSport, bike.OwnedSince()).Update("equipment_id", bike.ID).Error
	})
}

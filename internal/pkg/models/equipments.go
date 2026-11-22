package models

import (
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// /MEDIA/user uuid/equipments/equipment uuid/pic filename.pic extension

type Equipments struct {
	ID             uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `sql:"index" json:"-"`
	Name           string     `json:"name"`
	Brand          string     `json:"brand"`
	EquipmentModel string     `json:"model"`
	DateOfPurchase time.Time  `json:"date_of_purchase"`
	InitialMileage int        `json:"initial_mileage"`
	Mileage        int        `json:"mileage"`
	Weight         int        `json:"weight"`
	User           Users      `json:"-"`
	UserID         int        `json:"user_id"`
	IsCommute      bool       `json:"is_commute"` // given to the commute rides, one per user
	Picture        string     `json:"picture"`    // URL path of its picture under /MEDIA, empty without
	// DefaultSports are the sports it is the default equipment of (equipment_defaults), sorted. Nil in a
	// request keeps them, empty removes them.
	DefaultSports []string `json:"default_sports" gorm:"-"`
}

// EquipmentDefault is the default equipment of a sport of a user, given to the activities of that sport at
// their import
type EquipmentDefault struct {
	UserID      uint      `gorm:"primaryKey;autoIncrement:false"`
	Sport       string    `gorm:"primaryKey"`
	EquipmentID uuid.UUID `gorm:"type:uuid"`
}

// CommuteSport is the sport of the activities getting the commute equipment
const CommuteSport = "cycling"

// keepOnlyFlag clears flag (is_commute) on the other equipments of the user of equipment
func keepOnlyFlag(tx *gorm.DB, equipment *Equipments, flag string) error {
	query := tx.Model(&Equipments{}).Where("user_id = ?", equipment.UserID)
	if equipment.ID != uuid.Nil {
		query = query.Where("id <> ?", equipment.ID)
	}
	return query.Update(flag, false).Error
}

// CreateEquipment saves a new equipment, the default one of its DefaultSports (taken from the other
// equipments of the user), and the only commute one when IsCommute is set
func (db *DB) CreateEquipment(equipment *Equipments) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := keepOnlyCommute(tx, equipment); err != nil {
			return err
		}
		if err := tx.Omit("User").Create(equipment).Error; err != nil {
			return err
		}
		return setDefaultSports(tx, equipment)
	})
}

func keepOnlyCommute(tx *gorm.DB, equipment *Equipments) error {
	if equipment.IsCommute {
		return keepOnlyFlag(tx, equipment, "is_commute")
	}
	return nil
}

// setDefaultSports makes an equipment the default one of its DefaultSports, and of those only: they stop
// being the default of the other equipments of the user. Nil DefaultSports changes nothing.
func setDefaultSports(tx *gorm.DB, equipment *Equipments) error {
	if equipment.DefaultSports == nil {
		return nil
	}
	sports := uniqueSorted(equipment.DefaultSports)
	if err := tx.Where("equipment_id = ?", equipment.ID).Delete(&EquipmentDefault{}).Error; err != nil {
		return err
	}
	if len(sports) > 0 {
		err := tx.Where("user_id = ? AND sport IN ?", equipment.UserID, sports).Delete(&EquipmentDefault{}).Error
		if err != nil {
			return err
		}
		rows := make([]EquipmentDefault, len(sports))
		for i, sport := range sports {
			rows[i] = EquipmentDefault{UserID: uint(equipment.UserID), Sport: sport, EquipmentID: equipment.ID}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
	}
	equipment.DefaultSports = sports
	return nil
}

func uniqueSorted(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

// defaultSports returns the default sports of the equipments of a user, sorted, by equipment
func (db *DB) defaultSports(userID uint) (map[uuid.UUID][]string, error) {
	var rows []EquipmentDefault
	if err := db.Where("user_id = ?", userID).Order("sport").Find(&rows).Error; err != nil {
		return nil, err
	}
	sports := make(map[uuid.UUID][]string)
	for _, row := range rows {
		sports[row.EquipmentID] = append(sports[row.EquipmentID], row.Sport)
	}
	return sports, nil
}

// withDefaultSports sets the DefaultSports of an equipment, empty without
func (db *DB) withDefaultSports(equipment *Equipments) error {
	sports, err := db.defaultSports(uint(equipment.UserID))
	if err != nil {
		return err
	}
	equipment.DefaultSports = sports[equipment.ID]
	if equipment.DefaultSports == nil {
		equipment.DefaultSports = []string{}
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

// ActivitySports returns the sports of the activities of a user, sorted
func (db *DB) ActivitySports(userID uint) ([]string, error) {
	sports := []string{}
	err := db.Table("activities").Where("user_id = ?", userID).Distinct("sport").Order("sport").
		Pluck("sport", &sports).Error
	return sports, err
}

// UserSports returns the sports of the activities of a user and of their default equipments, sorted
func (db *DB) UserSports(userID uint) ([]string, error) {
	sports := []string{}
	err := db.Raw("SELECT sport FROM activities WHERE user_id = ? UNION "+
		"SELECT sport FROM equipment_defaults WHERE user_id = ? ORDER BY sport", userID, userID).
		Pluck("sport", &sports).Error
	return sports, err
}

// DefaultEquipment returns the default equipment of a sport of a user, nil when there is none
func (db *DB) DefaultEquipment(userID uint, sport string) (*Equipments, error) {
	var equipments []Equipments
	err := db.Where("id = (SELECT equipment_id FROM equipment_defaults WHERE user_id = ? AND sport = ?)",
		userID, sport).Limit(1).Find(&equipments).Error
	if err != nil || len(equipments) == 0 {
		return nil, err
	}
	return &equipments[0], nil
}

// UpdateEquipment saves an equipment, the default one of its DefaultSports (taken from the other
// equipments of the user, nil keeps them), and the only commute one when IsCommute is set
func (db *DB) UpdateEquipment(equipment *Equipments) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := keepOnlyCommute(tx, equipment); err != nil {
			return err
		}
		if err := tx.Omit("User").Save(equipment).Error; err != nil {
			return err
		}
		return setDefaultSports(tx, equipment)
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
	return equipment, db.withDefaultSports(&equipment)
}

// EquipmentStats is an equipment with the totals of its activities
type EquipmentStats struct {
	Equipments
	Activities int64      `json:"activities"`
	Distance   float64    `json:"distance"` // meters, of the activities only
	Duration   int64      `json:"duration"` // seconds
	LastUsed   *time.Time `json:"last_used"`
	// LastService is the date of the latest service, nil without one
	LastService *time.Time `json:"last_service"`
	// KmSinceService is the distance of the activities after the latest service, or of all of them without one
	KmSinceService       int     `json:"km_since_service"`
	DistanceSinceService float64 `json:"-"` // meters
	// Reminders are its maintenance reminders, the most urgent first
	Reminders []ReminderStatus `json:"reminders" gorm:"-"`
}

// GetEquipmentsStats returns the equipments of a user with their totals, the default ones first.
// The mileage is computed: the initial one plus the distance of the activities, in km.
func (db *DB) GetEquipmentsStats(userID uint) ([]EquipmentStats, error) {
	stats, err := db.equipmentsStats("e.user_id = ?", userID)
	if err != nil {
		return stats, err
	}
	reminders, err := db.GetUserReminders(userID)
	if err != nil {
		return stats, err
	}
	byEquipment := map[uuid.UUID][]ReminderStatus{}
	for _, r := range reminders {
		byEquipment[r.EquipmentID] = append(byEquipment[r.EquipmentID], r)
	}
	for i := range stats {
		stats[i].Reminders = byEquipment[stats[i].ID]
		if stats[i].Reminders == nil {
			stats[i].Reminders = []ReminderStatus{}
		}
	}
	return stats, nil
}

func (db *DB) GetEquipmentStats(userID uint, id uuid.UUID) (EquipmentStats, error) {
	stats, err := db.equipmentsStats("e.user_id = ? AND e.id = ?", userID, id)
	if err != nil {
		return EquipmentStats{}, err
	}
	if len(stats) == 0 {
		return EquipmentStats{}, gorm.ErrRecordNotFound
	}
	stats[0].Reminders, err = db.GetEquipmentReminders(id)
	return stats[0], err
}

func (db *DB) equipmentsStats(where string, args ...interface{}) ([]EquipmentStats, error) {
	stats := []EquipmentStats{}
	err := db.Raw("SELECT e.*, COUNT(a.id) AS activities, COALESCE(SUM(a.distance), 0) AS distance, "+
		"COALESCE(SUM(a.duration), 0) AS duration, MAX(a.date) AS last_used, MAX(s.last_service) AS last_service, "+
		"COALESCE(SUM(a.distance) FILTER (WHERE s.last_service IS NULL OR a.date > s.last_service), 0) "+
		"AS distance_since_service "+
		"FROM equipments e LEFT JOIN activities a ON a.equipment_id = e.id "+
		"LEFT JOIN (SELECT equipment_id, MAX(date) AS last_service FROM services GROUP BY equipment_id) s "+
		"ON s.equipment_id = e.id "+
		"WHERE "+where+" GROUP BY e.id ORDER BY "+
		"EXISTS (SELECT 1 FROM equipment_defaults d WHERE d.equipment_id = e.id) DESC, e.name", args...).
		Scan(&stats).Error
	if err != nil {
		return stats, err
	}
	sports := map[uint]map[uuid.UUID][]string{}
	for i := range stats {
		userID := uint(stats[i].UserID)
		if _, ok := sports[userID]; !ok {
			if sports[userID], err = db.defaultSports(userID); err != nil {
				return stats, err
			}
		}
		stats[i].DefaultSports = sports[userID][stats[i].ID]
		if stats[i].DefaultSports == nil {
			stats[i].DefaultSports = []string{}
		}
		stats[i].Mileage = stats[i].InitialMileage + int(math.Round(stats[i].Distance/1000))
		stats[i].KmSinceService = int(math.Round(stats[i].DistanceSinceService / 1000))
	}
	return stats, err
}

// DeleteEquipment deletes an equipment and its services, its activities are kept without equipment
func (db *DB) DeleteEquipment(uuid uuid.UUID) error {
	return db.Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Activity{}).Where("equipment_id = ?", uuid).Update("equipment_id", nil).Error
		if err != nil {
			return err
		}
		if err := tx.Where("equipment_id = ?", uuid).Delete(&Services{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", uuid).Delete(&Equipments{}).Error
	})
}

// GetEquipmentWeight returns the weight of an equipment (kg), 0 when it has none or doesn't exist
func (db *DB) GetEquipmentWeight(id uuid.UUID) (int, error) {
	var weights []int
	err := db.Model(&Equipments{}).Where("id = ?", id).Pluck("weight", &weights).Error
	if err != nil || len(weights) == 0 || weights[0] < 0 {
		return 0, err
	}
	return weights[0], nil
}

// ActivityWeightRow is what the weight of an activity depends on, and the weight it was analysed with
type ActivityWeightRow struct {
	ID              uuid.UUID
	UserID          uint
	Date            time.Time
	Sport           string
	TotalWeight     uint16
	EquipmentWeight int // 0 without equipment, or without weight
}

// GetActivityWeights returns, oldest first, what the weight of every activity depends on
func (db *DB) GetActivityWeights() ([]ActivityWeightRow, error) {
	var rows []ActivityWeightRow
	err := db.Table("activities AS a").
		Select("a.id, a.user_id, a.date, a.sport, COALESCE(a.total_weight, 0) AS total_weight, COALESCE(e.weight, 0) AS equipment_weight").
		Joins("LEFT JOIN equipments AS e ON e.id = a.equipment_id").Order("a.date").Scan(&rows).Error
	return rows, err
}

// SetActivityTotalWeight saves the weight an activity was analysed with, without analysing it again
func (db *DB) SetActivityTotalWeight(id uuid.UUID, weight uint16) error {
	return db.Model(&Activity{}).Where("id = ?", id).Update("total_weight", weight).Error
}

// sameCycle tells whether two cycles (dates it was last done, nil before the first time) are one
func sameCycle(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

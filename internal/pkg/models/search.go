package models

import (
	"MySportWeb/internal/pkg/types"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ActivityFilter selects activities, zero values meaning no condition
type ActivityFilter struct {
	Sports      []string
	From, To    time.Time // from included, to excluded
	Query       string    // in the title, case insensitive
	EquipmentID *uuid.UUID
	NoEquipment bool
	Device      string // the name of the recording device
	NoDevice    bool   // the activities whose file doesn't tell it
	Commute     *bool  // only the commutes, or only the other activities
}

// SortColumns are the columns the activities can be sorted by
var SortColumns = map[string]string{
	"date":      "activities.date",
	"title":     "activities.title",
	"distance":  "activities.distance",
	"duration":  "activities.duration",
	"speed":     "activities.avg_speed",
	"elevation": "activities.positive_elevation",
}

func (f ActivityFilter) apply(query *gorm.DB, userID uint) *gorm.DB {
	query = query.Where("activities.user_id = ?", userID)
	if len(f.Sports) > 0 {
		query = query.Where("activities.sport IN ?", f.Sports)
	}
	if !f.From.IsZero() {
		query = query.Where("activities.date >= ?", f.From)
	}
	if !f.To.IsZero() {
		query = query.Where("activities.date < ?", f.To)
	}
	if f.Query != "" {
		// the user's text is a value, not a pattern
		escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Query)
		query = query.Where("activities.title ILIKE ?", "%"+escaped+"%")
	}
	if f.NoEquipment {
		query = query.Where("activities.equipment_id IS NULL")
	} else if f.EquipmentID != nil {
		query = query.Where("activities.equipment_id = ?", *f.EquipmentID)
	}
	if f.NoDevice {
		// NULL: imported before the devices were read, until the re-analysis
		query = query.Where("activities.device_name IS NULL OR activities.device_name = ''")
	} else if f.Device != "" {
		query = query.Where("activities.device_name = ?", f.Device)
	}
	if f.Commute != nil {
		query = query.Where("activities.is_commute = ?", *f.Commute)
	}
	return query
}

// SearchActivities returns a page of the user's activities matching filter, sorted by sort (a key of
// SortColumns), and the totals of all the matching activities. Years are computed in loc.
func (db *DB) SearchActivities(userID uint, filter ActivityFilter, sort string, descending bool,
	page, perPage int, loc *time.Location) (types.ActivitySearch, error) {
	search := types.ActivitySearch{Activities: []types.ActivityRow{}, Sports: []string{}, Years: []int{},
		Devices: []types.DeviceCount{}}

	err := filter.apply(db.Table("activities"), userID).Select(totalsColumns).Scan(&search.Totals).Error
	if err != nil {
		return search, err
	}
	search.Total = search.Totals.Activities

	column, ok := SortColumns[sort]
	if !ok {
		column = SortColumns["date"]
	}
	order := column + " ASC"
	if descending {
		order = column + " DESC"
	}
	err = filter.apply(db.Table("activities"), userID).
		Select("activities.id, activities.title, activities.date, activities.sport, activities.distance, " +
			"activities.duration, activities.avg_speed, activities.positive_elevation AS elevation, " +
			"activities.is_commute, activities.equipment_id, equipments.name AS equipment_name, " +
			"COALESCE(activities.device_name, '') AS device_name").
		Joins("LEFT JOIN equipments ON equipments.id = activities.equipment_id").
		// the date, then the id, keep the order stable between pages
		Order(order).Order("activities.date DESC").Order("activities.id").
		Offset((page - 1) * perPage).Limit(perPage).Scan(&search.Activities).Error
	if err != nil {
		return search, err
	}

	err = db.Table("activities").Where("user_id = ?", userID).Distinct("sport").Order("sport").
		Pluck("sport", &search.Sports).Error
	if err != nil {
		return search, err
	}
	// the most used first
	err = db.Table("activities").Select("device_name AS name, COUNT(*) AS activities").
		Where("user_id = ? AND device_name <> ''", userID).Group("device_name").
		Order("COUNT(*) DESC").Order("device_name").Scan(&search.Devices).Error
	if err != nil {
		return search, err
	}
	err = db.Raw("SELECT DISTINCT extract(year FROM date AT TIME ZONE ?)::int AS year FROM activities "+
		"WHERE user_id = ? ORDER BY year DESC", loc.String(), userID).Pluck("year", &search.Years).Error
	return search, err
}

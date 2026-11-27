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
	Device      string     // the name of the recording device
	DeviceID    *uuid.UUID // the recording device: a unit, when the name is the one of a model
	NoDevice    bool       // the activities whose file doesn't tell it
	Commute     *bool      // only the commutes, or only the other activities
}

// SortColumns are the columns the activities can be sorted by
var SortColumns = map[string]string{
	"date":      "activities.date",
	"title":     "activities.title",
	"distance":  "activities.distance",
	"duration":  "activities.duration",
	"speed":     "activities.avg_speed",
	"elevation": "activities.positive_elevation",
	// the columns the user may add
	"moving_time":  "activities.moving_time",
	"moving_speed": "activities.avg_moving_speed",
	"max_speed":    "activities.max_speed",
	"descent":      "activities.negative_elevation",
	"avg_hr":       "activities.avg_hr",
	"power":        "activities.avg_power",
	"cadence":      "activities.avg_rpm",
	"calories":     "activities.calories",
	"co2":          "activities.co2",
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
	} else if f.DeviceID != nil {
		query = query.Where("activities.device_id = ?", *f.DeviceID)
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
	search := types.ActivitySearch{Activities: []types.ActivitySearchRow{}, Sports: []string{}, Years: []int{},
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
	// the activities without the measure (moving time not computed yet) last either way
	order := column + " ASC NULLS LAST"
	if descending {
		order = column + " DESC NULLS LAST"
	}
	err = filter.apply(db.Table("activities"), userID).
		Select("activities.id, activities.title, activities.date, activities.sport, activities.distance, " +
			"activities.duration, activities.avg_speed, activities.positive_elevation AS elevation, " +
			"activities.is_commute, activities.equipment_id, equipments.name AS equipment_name, " +
			"COALESCE(" + deviceDisplayName("devices") + ", activities.device_name, '') AS device_name, " +
			"(SELECT count(*) FROM cheers WHERE cheers.activity_id = activities.id) AS cheer_count, " +
			"(SELECT count(*) FROM comments WHERE comments.activity_id = activities.id) AS comment_count, " +
			"activities.moving_time, activities.avg_moving_speed, activities.max_speed, " +
			"activities.negative_elevation AS descent, activities.avg_hr, activities.avg_power, activities.avg_rpm, " +
			"activities.calories, activities.co2, " +
			HasMapColumn).
		Joins("LEFT JOIN equipments ON equipments.id = activities.equipment_id").
		Joins("LEFT JOIN devices ON devices.id = activities.device_id").
		// the date, then the id, keep the order stable between pages
		Order(order).Order("activities.date DESC").Order("activities.id").
		Offset((page - 1) * perPage).Limit(perPage).Scan(&search.Activities).Error
	if err != nil {
		return search, err
	}

	search.Sports, err = db.ActivitySports(userID)
	if err != nil {
		return search, err
	}
	// the most used first
	err = db.Table("devices").Select("devices.id, "+deviceDisplayName("devices")+" AS name, devices.serial, COUNT(*) AS activities").
		Joins("JOIN activities ON activities.device_id = devices.id").Where("devices.user_id = ?", userID).
		Group("devices.id").Order("COUNT(*) DESC").Order(deviceDisplayName("devices")).Order("devices.serial").
		Scan(&search.Devices).Error
	if err != nil {
		return search, err
	}
	err = db.Raw("SELECT DISTINCT extract(year FROM date AT TIME ZONE ?)::int AS year FROM activities "+
		"WHERE user_id = ? ORDER BY year DESC", loc.String(), userID).Pluck("year", &search.Years).Error
	return search, err
}

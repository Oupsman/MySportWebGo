package models

import (
	"MySportWeb/internal/pkg/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Activity struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time        `sql:"index"`
	User             Users             `json:"user"`
	UserID           uint              `json:"user_id"`
	Title            string            `json:"title"`
	Date             time.Time         `json:"date"`
	Filename         string            `json:"filename"`
	FilePath         string            `json:"file_path"`
	Sport            string            `json:"sport"`
	IsCommute        bool              `json:"is_commute"`
	Co2              float32           `json:"co2"`
	Distance         float64           `json:"distance"`
	Duration         uint32            `json:"duration"`
	AvgSpeed         float64           `json:"avg_speed"`
	AvgRPM           uint8             `json:"avg_rpm"`
	AvgHR            uint8             `json:"avg_hr"`
	Calories         uint16            `json:"kcal"`
	Timezone         string            `json:"timezone" gorm:"default:'Europe/Paris'"`
	Masked           bool              `json:"masked" gorm:"default:true"`
	Equipment        Equipments        `json:"equipment" gorm:"null"`
	EquipmentID      *uuid.UUID        `json:"equipment_id" gorm:"type:uuid"` // nil when the user has no equipment
	Serialnumber     string            `json:"serialnumber"`
	Lastimport       bool              `json:"lastimport"`
	Visibility       int               `json:"visibility" gorm:"default:0"`
	CanComments      bool              `json:"can_comments" gorm:"default:false"`
	EngineVersion    int               `json:"engine_version"`
	Medias           types.MultiString `json:"medias"`
	MaxSpeed         float64           `json:"max_speed"`
	MaxSpeedPosition types.GpsPoint    `json:"max_speed_position"`
	TotalWeight      uint16            `json:"total_weight"` // weight of the user + equipment at the time of the initial import of activity
	Device           types.Device      `json:"device"`
	UserAge          uint8             `json:"user_age"` // age of the user at the time of the initial import of activity
	Thumbnail        string            `json:"thumbnail"`
	// precalculated FIT analysis results
	Speeds              types.FloatArray    `json:"speeds"`
	Hearts              types.Uint16Array   `json:"hearts"`
	Powers              types.Uint16Array   `json:"powers"`
	PowerTS             types.MultiString   `json:"power_ts"`
	AvgPower            uint16              `json:"power_avg"`
	PowerAxis           types.FloatArray    `json:"power_axis"`
	Cadences            types.Uint16Array   `json:"cadences"`
	Distances           types.FloatArray    `json:"dist_array"`
	Lats                types.FloatArray    `json:"lats"`
	Lons                types.FloatArray    `json:"lons"`
	StepLengths         types.FloatArray    `json:"step_lengths"`
	GpsPoints           types.GpsPointArray `json:"gps_points"`
	PublicGpsPoints     types.GpsPointArray `json:"public_gps_points"`
	SimplifiedGpsPoints types.GpsPointArray `json:"-"` // Douglas-Peucker simplified trace, used to find similar activities
	RouteID             *uuid.UUID          `json:"route_id" gorm:"type:uuid;index"`
	VerticalOscis       types.FloatArray    `json:"vertical_oscis"`
	VerticalRatios      types.FloatArray    `json:"vertical_ratios"`
	Stances             types.FloatArray    `json:"stance"`
	StanceTimes         types.FloatArray    `json:"stance_times"`
	Temperatures        types.Int16Array    `json:"temperatures"`
	TimeStamps          types.MultiString   `json:"time_stamps"`
	PhysicalConditions  types.FloatArray    `json:"physical_conditions"`
	TotalAscent         uint16              `json:"total_ascent"`
	TotalDescent        uint16              `json:"total_descent"`
	Altitudes           types.FloatArray    `json:"altitudes"`
	GpsCenter           *types.GpsPoint     `json:"gps_center"`
	GpsBounds           types.GpsPointArray `json:"gps_bounds"`
	Means               types.FloatArray    `json:"means"`
	PositiveElevation   float64             `json:"positive_elevation"`
	NegativeElevation   float64             `json:"negative_elevation"`
	StartPosition       *types.GpsPoint     `json:"start_position"`
	EndPosition         *types.GpsPoint     `json:"end_position"`
	Lengths             types.LengthArray   `json:"lengths"`
}

func (db *DB) CreateActivity(activity *Activity) error {
	err := db.Create(activity).Error
	if err != nil {
		return err
	}
	return nil
}

func (db *DB) GetActivity(id uuid.UUID) (*Activity, error) {
	var activity Activity
	err := db.Preload("User").Preload("Equipment").First(&activity, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &activity, nil
}

func (db *DB) UpdateActivity(activity *Activity) error {
	// don't save the preloaded user and equipment along
	err := db.Omit(clause.Associations).Save(activity).Error
	if err != nil {
		return err
	}
	return nil
}

// DeleteActivity deletes the activity and updates its route, removing the route when it becomes empty
func (db *DB) DeleteActivity(id uuid.UUID) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var activity Activity
		err := tx.Select("id", "route_id").First(&activity, "id = ?", id).Error
		if err != nil {
			return err
		}
		err = tx.Delete(&Activity{}, "id = ?", id).Error
		if err != nil {
			return err
		}
		if activity.RouteID == nil {
			return nil
		}
		err = tx.Model(&Route{}).Where("id = ?", *activity.RouteID).
			Update("activity_count", gorm.Expr("activity_count - 1")).Error
		if err != nil {
			return err
		}
		return tx.Where("id = ? AND activity_count <= 0", *activity.RouteID).Delete(&Route{}).Error
	})
}

func (db *DB) GetActivitiesByUser(userID uint, start, number int) ([]types.ActivitySummary, error) {
	var activities []types.ActivitySummary
	err := db.Table("activities").Preload("User").Preload("Equipment").Where("user_id = ?", userID).Order("date desc").Offset(start).Limit(number).Scan(&activities).Error
	if err != nil {
		return nil, err
	}
	return activities, nil
}

func (db *DB) ResetImportStatus(userID uint) error {
	err := db.Debug().Table("activities").Where("user_id = ?", userID).Update("lastimport", false)
	if err != nil {
		return err.Error
	}
	return nil
}

// GetSimplifiedTrace returns the simplified trace of an activity, computing and saving it when missing
// (activities imported before the trace was stored)
func (db *DB) GetSimplifiedTrace(activity *Activity, simplify func([]types.GpsPoint) types.GpsPointArray) (types.GpsPointArray, error) {
	if len(activity.SimplifiedGpsPoints) > 0 {
		return activity.SimplifiedGpsPoints, nil
	}
	var full Activity
	err := db.Select("id", "gps_points").First(&full, "id = ?", activity.ID).Error
	if err != nil {
		return nil, err
	}
	if len(full.GpsPoints) < 2 {
		return nil, nil
	}
	activity.SimplifiedGpsPoints = simplify(full.GpsPoints)
	err = db.Model(&Activity{}).Where("id = ?", activity.ID).Update("simplified_gps_points", activity.SimplifiedGpsPoints).Error
	if err != nil {
		return nil, err
	}
	return activity.SimplifiedGpsPoints, nil
}

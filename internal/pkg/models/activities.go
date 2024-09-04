package models

import (
	"MySportWeb/internal/pkg/types"
	"github.com/google/uuid"
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
	EquipmentID      uuid.UUID         `json:"equipment_id"`
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
	Speeds             types.FloatArray    `json:"speeds"`
	Hearts             types.Uint16Array   `json:"hearts"`
	Powers             types.Uint16Array   `json:"powers"`
	AvgPower           uint16              `json:"power_avg"`
	PowerAxis          types.FloatArray    `json:"power_axis"`
	Cadences           types.Uint8Array    `json:"cadences"`
	Distances          types.FloatArray    `json:"dist_array"`
	Lats               types.FloatArray    `json:"lats"`
	Lons               types.FloatArray    `json:"lons"`
	StepLengths        types.FloatArray    `json:"step_lengths"`
	GpsPoints          types.GpsPointArray `json:"gps_points"`
	PublicGpsPoints    types.GpsPointArray `json:"public_gps_points"`
	VerticalOscis      types.FloatArray    `json:"vertical_oscis"`
	VerticalRatios     types.FloatArray    `json:"vertical_ratios"`
	Stances            types.FloatArray    `json:"stance"`
	StanceTimes        types.FloatArray    `json:"stance_times"`
	Temperatures       types.Int8Array     `json:"temperatures"`
	TimeStamps         types.MultiString   `json:"time_stamps"`
	PhysicalConditions types.FloatArray    `json:"physical_conditions"`
	TotalAscent        uint16              `json:"total_ascent"`
	TotalDescent       uint16              `json:"total_descent"`
	Altitudes          types.FloatArray    `json:"altitudes"`
	GpsCenter          *types.GpsPoint     `json:"gps_center"`
	GpsBounds          types.GpsPointArray `json:"gps_bounds"`
	Means              types.FloatArray    `json:"means"`
	PositiveElevation  float64             `json:"positive_elevation"`
	NegativeElevation  float64             `json:"negative_elevation"`
	StartPosition      *types.GpsPoint     `json:"start_position"`
	EndPosition        *types.GpsPoint     `json:"end_position"`
	Lengths            types.LengthArray   `json:"lengths"`
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
	err := db.Save(activity).Error
	if err != nil {
		return err
	}
	return nil
}

func (db *DB) DeleteActivity(id uuid.UUID) error {
	err := db.Delete(&Activity{}, "id = ?", id).Error
	if err != nil {
		return err
	}
	return nil
}

func (db *DB) GetActivitiesByUser(userID uint) ([]types.ActivitySummary, error) {
	var activities []types.ActivitySummary
	err := db.Debug().Table("activities").Preload("User").Preload("Equipment").Where("user_id = ?", userID).Scan(&activities).Error
	if err != nil {
		return nil, err
	}
	return activities, nil
}

package models

import (
	"MySportWeb/pkg/internal/types"
	"github.com/google/uuid"
	"time"
)

type Activity struct {
	ID            uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time `sql:"index"`
	User          Users
	UserID        uint      `json:"user_id"`
	Title         string    `json:"title"`
	Date          time.Time `json:"date"`
	Filename      string    `json:"filename"`
	FilePath      string    `json:"file_path"`
	Sport         string    `json:"sport"`
	IsCommute     string    `json:"is_commute"`
	Co2           *int      `json:"co2"`
	Device        string    `json:"device"`
	Distance      *float64  `json:"distance"`
	Duration      int       `json:"duration"`
	AvgSpeed      *float64  `json:"avg_speed"`
	AvgRPM        *float64  `json:"avg_rpm"`
	AvgHR         *float64  `json:"avg_hr"`
	Kcal          *float64  `json:"kcal"`
	Timezone      string    `json:"timezone" gorm:"default:'Europe/Paris'"`
	Masked        bool      `json:"masked" gorm:"default:true"`
	Equipment     Equipments
	EquipmentID   int    `json:"equipment_id"`
	Serialnumber  string `json:"serialnumber"`
	Lastimport    bool   `json:"lastimport"`
	Visibility    int    `json:"visibility" gorm:"default:0"`
	CanComments   bool   `json:"can_comments" gorm:"default:false"`
	EngineVersion int    `json:"engine_version"`
	// precalculated FIT analysis results
	Speeds             types.FloatArray   `json:"speeds"`
	Hearts             types.FloatArray   `json:"hearts"`
	Alts               types.FloatArray   `json:"alts"`
	Powers             types.FloatArray   `json:"powers"`
	PowerAvg           *float64           `json:"power_avg"`
	PowerAxis          types.FloatArray   `json:"power_axis"`
	Cadences           types.FloatArray   `json:"cadences"`
	Distances          types.FloatArray   `json:"dist_array"`
	Lats               types.FloatArray   `json:"lats"`
	Lons               types.FloatArray   `json:"lons"`
	StepLengths        types.FloatArray   `json:"step_lengths"`
	GpsPoints          []types.GpsPoint   `json:"gps_points"`
	VerticalOscis      types.FloatArray   `json:"vertical_oscis"`
	VerticalRatios     types.FloatArray   `json:"vertical_ratios"`
	Stances            types.FloatArray   `json:"stance"`
	StanceTimes        types.FloatArray   `json:"stance_times"`
	Temperatures       types.FloatArray   `json:"temperatures"`
	TimeStamps         []string           `json:"time_stamps"`
	PhysicalConditions []types.FloatArray `json:"physical_conditions"`
	PositiveElevation  *float64           `json:"positive_elevation"`
	GpsCenter          *types.GpsPoint    `json:"gps_center"`
	GpsBounds          []types.GpsPoint   `json:"gps_bounds"`
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

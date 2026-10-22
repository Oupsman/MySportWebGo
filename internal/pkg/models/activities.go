package models

import (
	"MySportWeb/internal/pkg/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// Co2PerMeter is the CO2 (in grams) a commute saves per meter compared to driving
const Co2PerMeter = 0.2217

type Activity struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
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
	// seconds since the start of each value of the sparse series, which skip the records without a value
	GpsTimeStamps      types.FloatArray    `json:"gps_time_stamps"`
	HeartTimeStamps    types.FloatArray    `json:"heart_time_stamps"`
	CadenceTimeStamps  types.FloatArray    `json:"cadence_time_stamps"`
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
	// only the listed columns: a full row holds every record of the FIT file
	err := db.Table("activities").Select("id", "title", "date", "sport", "thumbnail").
		Where("user_id = ?", userID).Order("date desc").Offset(start).Limit(number).Scan(&activities).Error
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

// GetActivitiesBelowEngine returns the ids of the activities analysed by an engine older than version
func (db *DB) GetActivitiesBelowEngine(version int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("engine_version < ? OR engine_version IS NULL", version).
		Order("date asc").Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ActivityToRetitle holds what naming an activity needs
type ActivityToRetitle struct {
	ID            uuid.UUID
	Title         string
	Sport         string
	Date          time.Time
	StartPosition *types.GpsPoint
	Timezone      string // the user's one
}

// GetActivitiesToRetitle returns the activities whose title is one of titles(their sport)
func (db *DB) GetActivitiesToRetitle(titles func(sport string) []string) ([]ActivityToRetitle, error) {
	var sports []string
	if err := db.Model(&Activity{}).Distinct().Pluck("sport", &sports).Error; err != nil {
		return nil, err
	}
	activities := []ActivityToRetitle{}
	if len(sports) == 0 {
		return activities, nil
	}
	condition := db.Where("1 = 0")
	for _, sport := range sports {
		condition = condition.Or("activities.sport = ? AND activities.title IN ?", sport, titles(sport))
	}
	err := db.Table("activities").Joins("JOIN users ON users.id = activities.user_id").
		Select("activities.id, activities.title, activities.sport, activities.date, activities.start_position, users.timezone").
		Where(condition).Order("activities.date").Scan(&activities).Error
	return activities, err
}

// SetActivityTitle renames an activity, unless its title isn't oldTitle anymore
func (db *DB) SetActivityTitle(id uuid.UUID, oldTitle, title string) error {
	return db.Model(&Activity{}).Where("id = ? AND title = ?", id, oldTitle).Update("title", title).Error
}

// GetActivityFilePath returns the path of the FIT file of an activity
func (db *DB) GetActivityFilePath(id uuid.UUID) (string, error) {
	var activity Activity
	err := db.Select("id", "file_path").First(&activity, "id = ?", id).Error
	return activity.FilePath, err
}

// UpdateActivityTimeStamps saves the time stamps of the sparse series and the engine version
func (db *DB) UpdateActivityTimeStamps(id uuid.UUID, analysed *Activity, version int) error {
	updates := map[string]interface{}{"engine_version": version}
	if analysed != nil {
		updates["gps_time_stamps"] = analysed.GpsTimeStamps
		updates["heart_time_stamps"] = analysed.HeartTimeStamps
		updates["cadence_time_stamps"] = analysed.CadenceTimeStamps
		updates["power_ts"] = analysed.PowerTS
	}
	return db.Model(&Activity{}).Where("id = ?", id).Updates(updates).Error
}

// CountActivitiesWithFile returns the number of activities analysed from the FIT file at path
func (db *DB) CountActivitiesWithFile(path string) (int64, error) {
	var count int64
	err := db.Model(&Activity{}).Where("file_path = ?", path).Count(&count).Error
	return count, err
}

// FindDuplicateActivity returns the id of the user's activity of the same sport starting at the same time,
// nil when there is none: the same FIT file, or another export of it
func (db *DB) FindDuplicateActivity(userID uint, sport string, date time.Time) (*uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("user_id = ? AND sport = ? AND date = ?", userID, sport, date).
		Limit(1).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return &ids[0], nil
}

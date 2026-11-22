package models

import (
	"MySportWeb/internal/pkg/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
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
	SourceFilePath   string            `json:"-" gorm:"not null;default:''"` // the GPX or TCX file FilePath was converted from, never served
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
	DeviceName       string            `json:"device_name" gorm:"index"`         // "Garmin Edge 530", to filter on it
	DeviceID         *uuid.UUID        `json:"device_id" gorm:"type:uuid;index"` // nil when the file doesn't name the device
	UserAge          uint8             `json:"user_age"`                         // age of the user at the time of the initial import of activity
	Thumbnail        string            `json:"thumbnail"`
	// the version of the drawing of its thumbnail (activityService.ThumbnailVersion): an older one is drawn
	// again at the start
	ThumbnailVersion int `json:"-" gorm:"not null;default:0"`
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
	GpsPoints           types.GpsPointArray `json:"gps_points"`
	PublicGpsPoints     types.GpsPointArray `json:"public_gps_points"`
	SimplifiedGpsPoints types.GpsPointArray `json:"-"` // Douglas-Peucker simplified trace, used to find similar activities
	RouteID             *uuid.UUID          `json:"route_id" gorm:"type:uuid;index"`
	// the measures only some sensors or sports record (running dynamics...), absent without any
	Metrics      types.ActivityMetrics `json:"metrics" gorm:"type:jsonb"`
	Temperatures types.Int16Array      `json:"temperatures"`
	TimeStamps   types.MultiString     `json:"time_stamps"`
	// seconds since the start of each value of the sparse series, which skip the records without a value
	GpsTimeStamps         types.FloatArray    `json:"gps_time_stamps"`
	HeartTimeStamps       types.FloatArray    `json:"heart_time_stamps"`
	CadenceTimeStamps     types.FloatArray    `json:"cadence_time_stamps"`
	TemperatureTimeStamps types.FloatArray    `json:"temperature_time_stamps"`
	PhysicalConditions    types.FloatArray    `json:"physical_conditions"`
	TotalAscent           uint16              `json:"total_ascent"`
	TotalDescent          uint16              `json:"total_descent"`
	Altitudes             types.FloatArray    `json:"altitudes"`
	GpsCenter             *types.GpsPoint     `json:"gps_center"`
	GpsBounds             types.GpsPointArray `json:"gps_bounds"`
	Means                 types.FloatArray    `json:"means"`
	PositiveElevation     float64             `json:"positive_elevation"`
	NegativeElevation     float64             `json:"negative_elevation"`
	// the altitude correction applied to Altitudes (AltitudeCorrectionNone...): the one of the device, or none
	// when the ground elevation of the trace is unknown
	ElevationCorrection string            `json:"elevation_correction" gorm:"not null;default:'none'"`
	StartPosition       *types.GpsPoint   `json:"start_position"`
	EndPosition         *types.GpsPoint   `json:"end_position"`
	Lengths             types.LengthArray `json:"lengths"`
	Legs                types.LegArray    `json:"legs"` // the sessions of a multi-session file, empty for one session
	// seconds since the start after which the recording is ignored (the device wasn't stopped), 0 for none
	TrimEnd float64 `json:"trim_end"`
	// seconds between the start and the end of the whole recording, trimmed or not; 0 for the activities
	// imported before it was stored
	RecordedDuration float64 `json:"recorded_duration"`
	// training score: the one recorded by the device (Garmin's training load, else the TSS), else the
	// Banister TRIMP of the heart rate stream; 0 without any, nil until computed
	Trimp *float64 `json:"trimp"`
	// seconds in each heart rate reserve zone (activityService.HRZoneLimits), zeros without heart rate, nil
	// until computed; and the heart rates bounding them: resting, lower limits of the zones 2 to 5, maximum
	HrZones      types.FloatArray  `json:"hr_zones"`
	HrZoneBounds types.Uint16Array `json:"hr_zone_bounds"`
	// the fastest stretches over distances and the best powers over durations (activityService.BestEfforts),
	// empty without any, nil until computed
	BestEfforts types.BestEffortArray `json:"best_efforts"`
	// seconds in each power zone (activityService.PowerZoneLimits) of a ride with a power meter, zeros
	// without power or FTP, nil until computed; the watts bounding them: 0, the lower limits of the zones 2
	// to 7; the FTP (W) they were computed with, 0 without
	PowerZones      types.FloatArray  `json:"power_zones"`
	PowerZoneBounds types.Uint16Array `json:"power_zone_bounds"`
	FTP             uint16            `json:"ftp" gorm:"column:ftp;not null;default:0"`
	// the normalized power (W), the intensity factor (normalized power / FTP) and the training stress score
	// of a ride with a power meter, nil without
	NormalizedPower *float64 `json:"normalized_power"`
	Intensity       *float64 `json:"intensity"`
	TSS             *float64 `json:"tss" gorm:"column:tss"`
	// where Trimp comes from: ScoreDevice, ScorePower, ScoreHR; empty for the activities scored before
	ScoreSource string `json:"score_source" gorm:"not null;default:''"`
}

// The sources of the training score of an activity
const (
	ScoreDevice = "device" // recorded by the device: Garmin's training load, or its TSS
	ScorePower  = "power"  // the TSS of the measured power
	ScoreHR     = "hr"     // the Banister TRIMP of the heart rate
)

// HidePrivate removes what only the owner of the activity may see: the trace inside the privacy zones
// (every position but the public ones), where its files are, who recorded it and with what. The map of
// the others is centered on the public trace.
func (a *Activity) HidePrivate() {
	a.GpsPoints = nil
	a.GpsTimeStamps = nil
	a.SimplifiedGpsPoints = nil
	a.Lats = nil
	a.Lons = nil
	a.StartPosition = nil
	a.EndPosition = nil
	a.MaxSpeedPosition = types.GpsPoint{}
	a.GpsCenter, a.GpsBounds = traceBounds(a.PublicGpsPoints)

	a.Filename = ""
	a.FilePath = ""
	a.SourceFilePath = ""
	a.Thumbnail = ""
	a.User = Users{}
	a.UserID = 0
	a.Equipment = Equipments{}
	a.EquipmentID = nil
	a.RouteID = nil
	a.Serialnumber = ""
	a.Device.Serial = ""
	a.Lastimport = false
}

// traceBounds returns the center and the bounds (south-west, north-east) of a trace, nil without point
func traceBounds(points types.GpsPointArray) (*types.GpsPoint, types.GpsPointArray) {
	if len(points) == 0 {
		return nil, nil
	}
	min, max := points[0], points[0]
	var sum types.GpsPoint
	for _, p := range points {
		min.Lat, min.Lon = math.Min(min.Lat, p.Lat), math.Min(min.Lon, p.Lon)
		max.Lat, max.Lon = math.Max(max.Lat, p.Lat), math.Max(max.Lon, p.Lon)
		sum.Lat += p.Lat
		sum.Lon += p.Lon
	}
	n := float64(len(points))
	return &types.GpsPoint{Lat: sum.Lat / n, Lon: sum.Lon / n}, types.GpsPointArray{min, max}
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
		return (&DB{DB: *tx}).LeaveRoute(*activity.RouteID)
	})
}

// LeaveRoute counts one activity less on the route, removing the route when it becomes empty
func (db *DB) LeaveRoute(routeID uuid.UUID) error {
	err := db.Model(&Route{}).Where("id = ?", routeID).
		Update("activity_count", gorm.Expr("activity_count - 1")).Error
	if err != nil {
		return err
	}
	return db.Where("id = ? AND activity_count <= 0", routeID).Delete(&Route{}).Error
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
	err := db.Table("activities").Where("user_id = ?", userID).Update("lastimport", false)
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

// GetActivityToReanalyze returns what re-analysing an activity needs: its FIT file, where it is trimmed, its
// user (empty when deleted), its route, its equipment (the weight of the estimated power), and its traces to
// compute the public one again without the file
func (db *DB) GetActivityToReanalyze(id uuid.UUID) (Activity, error) {
	var activity Activity
	err := db.Select("id", "user_id", "file_path", "trim_end", "route_id", "gps_points", "public_gps_points",
		"equipment_id").Preload("User").First(&activity, "id = ?", id).Error
	return activity, err
}

// GetActivitiesWithoutTrimp returns the ids of the activities whose TRIMP was never computed, oldest first
func (db *DB) GetActivitiesWithoutTrimp() ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("trimp IS NULL").Order("date asc").Pluck("id", &ids).Error
	return ids, err
}

// GetActivityHearts returns an activity with only what its training score needs: its user, date, FIT file,
// trim and heart rate stream
func (db *DB) GetActivityHearts(id uuid.UUID) (Activity, error) {
	var activity Activity
	err := db.Select("id", "user_id", "date", "file_path", "trim_end", "hearts", "heart_time_stamps").Preload("User").
		First(&activity, "id = ?", id).Error
	return activity, err
}

// SetActivityTrimp saves the TRIMP of an activity
func (db *DB) SetActivityTrimp(id uuid.UUID, trimp float64) error {
	return db.Model(&Activity{}).Where("id = ?", id).Update("trimp", trimp).Error
}

// GetActivitiesWithoutHrZones returns the ids of the activities whose heart rate zones were never computed
// (NULL, or a nil array saved as JSON null), oldest first
func (db *DB) GetActivitiesWithoutHrZones() ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("hr_zones IS NULL OR hr_zones = 'null'").Order("date asc").Pluck("id", &ids).Error
	return ids, err
}

// SetActivityHrZones saves the time in heart rate zones of an activity and the heart rates bounding them
func (db *DB) SetActivityHrZones(id uuid.UUID, zones types.FloatArray, bounds types.Uint16Array) error {
	return db.Model(&Activity{}).Where("id = ?", id).
		Updates(map[string]interface{}{"hr_zones": zones, "hr_zone_bounds": bounds}).Error
}

// ResetHrZones forgets the heart rate zones of the activities of a user, to compute them again: their
// maximum heart rate changed
func (db *DB) ResetHrZones(userID uint) error {
	return db.Model(&Activity{}).Where("user_id = ?", userID).
		Updates(map[string]interface{}{"hr_zones": nil, "hr_zone_bounds": nil}).Error
}

// ActivityZones is the time in heart rate zones of an activity at its date
type ActivityZones struct {
	Date    time.Time
	HrZones types.FloatArray
}

// GetZonesSince returns the time in heart rate zones of the activities of a user since from, oldest first
func (db *DB) GetZonesSince(userID uint, from time.Time) ([]ActivityZones, error) {
	var zones []ActivityZones
	err := db.Model(&Activity{}).Select("date", "hr_zones").
		Where("user_id = ? AND date >= ? AND hr_zones IS NOT NULL", userID, from).
		Order("date asc").Scan(&zones).Error
	return zones, err
}

// UpdateActivityTimeStamps saves what an engine upgrade re-analyses (the time stamps of the sparse
// series, the recording device, the power, the temperature, the altitudes and the elevation, the start and
// end positions and the public trace) and the engine version. privacy alone, when not nil, holds the public trace computed again
// without the FIT file.
func (db *DB) UpdateActivityTimeStamps(id uuid.UUID, analysed *Activity, privacy *types.GpsPointArray, version int) error {
	updates := map[string]interface{}{"engine_version": version}
	if privacy != nil {
		updates["public_gps_points"] = *privacy
	}
	if analysed != nil {
		updates["public_gps_points"] = analysed.PublicGpsPoints
		updates["start_position"] = analysed.StartPosition
		updates["end_position"] = analysed.EndPosition
		updates["device"] = analysed.Device
		updates["device_name"] = analysed.DeviceName
		updates["device_id"] = analysed.DeviceID
		updates["gps_time_stamps"] = analysed.GpsTimeStamps
		updates["heart_time_stamps"] = analysed.HeartTimeStamps
		updates["cadence_time_stamps"] = analysed.CadenceTimeStamps
		updates["power_ts"] = analysed.PowerTS
		updates["powers"] = analysed.Powers
		updates["temperatures"] = analysed.Temperatures
		updates["temperature_time_stamps"] = analysed.TemperatureTimeStamps
		updates["recorded_duration"] = analysed.RecordedDuration
		updates["metrics"] = analysed.Metrics
		updates["best_efforts"] = analysed.BestEfforts
		// the power stream changed: BackfillPowerMetrics computes its metrics again
		updates["power_zones"] = nil
		for column, value := range elevationUpdates(analysed) {
			updates[column] = value
		}
	}
	return db.Model(&Activity{}).Where("id = ?", id).Updates(updates).Error
}

// elevationUpdates are the columns that depend on the altitudes: the profile, the elevation, and the power
// estimated from their slope (of the activity and of its legs) and from the weight it moves
func elevationUpdates(analysed *Activity) map[string]interface{} {
	return map[string]interface{}{
		"total_weight":         analysed.TotalWeight,
		"altitudes":            analysed.Altitudes,
		"positive_elevation":   analysed.PositiveElevation,
		"negative_elevation":   analysed.NegativeElevation,
		"elevation_correction": analysed.ElevationCorrection,
		"powers":               analysed.Powers,
		"power_ts":             analysed.PowerTS,
		"avg_power":            analysed.AvgPower,
		"legs":                 analysed.Legs,
	}
}

// UpdateActivityElevation saves what depends on the altitudes of an activity analysed again with the
// altitude correction of its device
func (db *DB) UpdateActivityElevation(id uuid.UUID, analysed *Activity) error {
	return db.Model(&Activity{}).Where("id = ?", id).Updates(elevationUpdates(analysed)).Error
}

// RenameActivity renames an activity of a user, false when the user has no such activity
func (db *DB) RenameActivity(userID uint, id uuid.UUID, title string) (bool, error) {
	result := db.Model(&Activity{}).Where("id = ? AND user_id = ?", id, userID).Update("title", title)
	return result.RowsAffected == 1, result.Error
}

// GetDeviceActivities returns the ids of the activities of a device of a user, oldest first
func (db *DB) GetDeviceActivities(userID uint, deviceID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("user_id = ? AND device_id = ?", userID, deviceID).Order("date").
		Pluck("id", &ids).Error
	return ids, err
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

// HasMapColumn is true for an activity whose public trace has two points at least: its thumbnail shows a
// map, the others are blank. The trace is JSON in a bytea.
const HasMapColumn = "(activities.public_gps_points IS NOT NULL AND " +
	"jsonb_path_exists(convert_from(activities.public_gps_points, 'UTF8')::jsonb, '$[1]')) AS has_map"

// ThumbnailToDraw is what drawing the thumbnail of an activity needs
type ThumbnailToDraw struct {
	ID              uuid.UUID
	UserUUID        uuid.UUID
	PublicGpsPoints types.GpsPointArray
}

// GetThumbnailsBelowVersion returns the ids of the activities whose thumbnail was drawn by an older version
// than version, most recent first: the ones the lists show first
func (db *DB) GetThumbnailsBelowVersion(version int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("thumbnail_version < ?", version).Order("date DESC").Pluck("id", &ids).Error
	return ids, err
}

// GetThumbnailToDraw returns what drawing the thumbnail of an activity needs; a nil UserUUID for an
// activity without user
func (db *DB) GetThumbnailToDraw(id uuid.UUID) (ThumbnailToDraw, error) {
	var thumbnail ThumbnailToDraw
	err := db.Table("activities").
		Select("activities.id, users.uuid AS user_uuid, activities.public_gps_points").
		Joins("LEFT JOIN users ON users.id = activities.user_id").
		Where("activities.id = ?", id).Take(&thumbnail).Error
	return thumbnail, err
}

// SetThumbnailVersion records the version of the drawing of the thumbnail of an activity
func (db *DB) SetThumbnailVersion(id uuid.UUID, version int) error {
	return db.Model(&Activity{}).Where("id = ?", id).UpdateColumn("thumbnail_version", version).Error
}

// MediaAccess is who may see the thumbnail of an activity: its owner, and the others by its visibility
type MediaAccess struct {
	OwnerUUID  uuid.UUID
	Visibility int
}

// GetMediaAccess returns the owner and the visibility of an activity
func (db *DB) GetMediaAccess(id uuid.UUID) (MediaAccess, error) {
	var access MediaAccess
	err := db.Table("activities").Select("users.uuid AS owner_uuid, activities.visibility").
		Joins("JOIN users ON users.id = activities.user_id").
		Where("activities.id = ?", id).Take(&access).Error
	return access, err
}

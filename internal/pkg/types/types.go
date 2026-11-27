package types

import (
	"database/sql/driver"
	"encoding/json"
	// "fmt" // used by the dead code below
	"github.com/google/uuid"
	"mime/multipart"
	// "strconv" // used by the dead code below
	// "strings" // used by the dead code below
	"time"
)

type MultiString []string

type NotificationsBody struct {
	Message         string `json:"message"`
	Config          string `json:"config"`
	ChannelProvider string `json:"channel_provider"`
	ID              uint   `json:"id"`
	ChannelID       uint   `json:"channel_id"`
	UserID          uint   `json:"user_id"`
}

var Genders = []string{"Male", "Female"}

var Units = []string{"Imperial", "Metric"}

type FloatArray []float64

type GpsPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type GpsPointArray []GpsPoint

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// type ChannelBody struct {
// 	ChannelID       uint64 `json:"channel_id"`
// 	ChannelProvider string `json:"channel_provider"`
// 	Config          string `json:"config"`
// 	Name            string `json:"name"`
// 	UserID          uint64 `json:"user_id"`
// }

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// type UserBody struct {
// 	ID                   uint64    `json:"user_id"`
// 	UUID                 uuid.UUID `json:"uuid"`
// 	Username             string    `json:"username"`
// 	Password             string    `json:"newPassword"`
// 	PasswordConfirmation string    `json:"newPasswordConfirmation"`
// 	OldPassword          string    `json:"oldpassword"`
// 	DateOfBirth          time.Time `json:"date_of_birth"`
// 	Email                string    `json:"email"`
// 	Weight               uint16    `json:"weight"`
// 	Unit                 int       `json:"unit"`
// 	Timezone             string    `json:"timezone"`
// 	Gender               int       `json:"gender"`
// 	Height               uint16    `json:"height"`
// 	MaxHR                uint16    `json:"max_hr"`
// 	WeightObjective      float64   `json:"weight_objective"`
// 	SecurityDistance     uint16    `json:"security_distance"`
// 	HomeTown             string    `json:"home_town"`
// }

type LoginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RegisterBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	// Invitation is the code of an invitation, required when REGISTRATION=invite
	Invitation string `json:"invitation"`
	// SetupCode is the code of the logs that registers the first admin
	SetupCode string `json:"setup_code"`
}

// Device is the device that recorded an activity, as its FIT file tells
type Device struct {
	Model    string `json:"model"`
	Brand    string `json:"brand"`
	Serial   string `json:"serial_number"`
	Software string `json:"software"` // the version of its firmware
}

type Length struct {
	Swolf     uint16  `json:"swolf"`
	Pace      float64 `json:"pace"`
	Length    float64 `json:"length"`
	Strokes   uint16  `json:"strokes"`
	Duration  float64 `json:"duration"`
	TimeStamp float64 `json:"timeStamp"`
}

// Leg is a session of a multi-session activity (a triathlon...). The transitions (T1, T2) are not legs:
// their time is in the TransitionTime of the leg after them, and in the duration of the activity.
type Leg struct {
	Sport          string  `json:"sport"`
	StartOffset    float64 `json:"start_offset"` // seconds since the start of the activity
	Duration       float64 `json:"duration"`     // timer time, in seconds
	Distance       float64 `json:"distance"`     // meters
	AvgSpeed       float64 `json:"avg_speed"`    // m/s
	MaxSpeed       float64 `json:"max_speed"`
	AvgHR          uint8   `json:"avg_hr"`
	MaxHR          uint8   `json:"max_hr"`
	AvgCadence     uint8   `json:"avg_cadence"`
	AvgPower       uint16  `json:"avg_power"`
	PowerEstimated bool    `json:"power_estimated"` // from the speed and the slope, without power meter
	Calories       uint16  `json:"kcal"`
	Ascent         uint16  `json:"ascent"`
	Descent        uint16  `json:"descent"`
	TransitionTime float64 `json:"transition_time"` // seconds of the transitions just before the leg
}

type LegArray []Leg

type ActivitySummary struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Date      time.Time `json:"date"`
	Sport     string    `json:"sport"`
	Thumbnail string    `json:"thumbnail"`
	// the cheers and the comments of the activity
	CheerCount   int64 `json:"cheer_count"`
	CommentCount int64 `json:"comment_count"`
}

// RouteActivity is an activity of a route, with what is needed to compare it to the others
type RouteActivity struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Date     time.Time `json:"date"`
	Distance float64   `json:"distance"`
	Duration uint32    `json:"duration"`
	AvgSpeed float64   `json:"avg_speed"`
	// the time and the speed while moving: the activities compare on them, whether their device paused at the
	// stops or not; nil until computed, 0 when the records don't tell
	MovingTime     *float64 `json:"moving_time"`
	AvgMovingSpeed *float64 `json:"avg_moving_speed"`
}

type ActivityCalendar struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Date     time.Time `json:"start"`
	Sport    string    `json:"sport"`
	Distance float64   `json:"distance"`
	Duration int       `json:"duration"`
}

// PeriodTotals sums the activities of a period: distance and elevation in meters, duration in seconds
type PeriodTotals struct {
	Activities int64   `json:"activities"`
	Distance   float64 `json:"distance"`
	Duration   int64   `json:"duration"`
	Elevation  float64 `json:"elevation"`
}

// DashboardTotals compares the current periods with the previous complete ones
type DashboardTotals struct {
	ThisWeek  PeriodTotals `json:"this_week"`
	LastWeek  PeriodTotals `json:"last_week"`
	ThisMonth PeriodTotals `json:"this_month"`
	LastMonth PeriodTotals `json:"last_month"`
	ThisYear  PeriodTotals `json:"this_year"`
	AllTime   PeriodTotals `json:"all_time"`
}

// CommuteTotals sums the commutes of a period: distance in meters, CO2 saved compared to driving in grams
type CommuteTotals struct {
	Activities int64   `json:"activities"`
	Distance   float64 `json:"distance"`
	Co2        float64 `json:"co2"`
}

type DashboardCommutes struct {
	ThisYear CommuteTotals `json:"this_year"`
	AllTime  CommuteTotals `json:"all_time"`
}

// WeeklyVolume sums the activities of a sport during a week (week: its Monday, YYYY-MM-DD)
type WeeklyVolume struct {
	Week       string  `json:"week"`
	Sport      string  `json:"sport"`
	Activities int64   `json:"activities"`
	Distance   float64 `json:"distance"`
	Duration   int64   `json:"duration"`
}

// SportRecord is the activity holding a record, Value in the record unit (meters or seconds)
type SportRecord struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Date  time.Time `json:"date"`
	Value float64   `json:"value"`
}

type SportSummary struct {
	Sport           string       `json:"sport"`
	Totals          PeriodTotals `json:"totals"`
	LongestDistance *SportRecord `json:"longest_distance"`
	LongestDuration *SportRecord `json:"longest_duration"`
	BiggestClimb    *SportRecord `json:"biggest_climb"`
}

type RecentActivity struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Date      time.Time `json:"date"`
	Sport     string    `json:"sport"`
	Distance  float64   `json:"distance"`
	Duration  uint32    `json:"duration"`
	AvgSpeed  float64   `json:"avg_speed"`
	Elevation float64   `json:"elevation"`
	HasMap    bool      `json:"-"`
	// the URL of its thumbnail, "" without map (indoor, or a short loop inside the privacy zones)
	Thumbnail string `json:"thumbnail"`
}

// ActivityRow is an activity of the activities table
type ActivityRow struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	Date          time.Time  `json:"date"`
	Sport         string     `json:"sport"`
	Distance      float64    `json:"distance"`
	Duration      uint32     `json:"duration"`
	AvgSpeed      float64    `json:"avg_speed"`
	Elevation     float64    `json:"elevation"`
	IsCommute     bool       `json:"is_commute"`
	EquipmentID   *uuid.UUID `json:"equipment_id"`
	EquipmentName *string    `json:"equipment_name"`
	DeviceName    string     `json:"device_name"`
	HasMap        bool       `json:"-"`
	// the URL of its thumbnail, "" without map (indoor, or a short loop inside the privacy zones)
	Thumbnail string `json:"thumbnail"`
	// the cheers and the comments of the activity
	CheerCount   int64 `json:"cheer_count"`
	CommentCount int64 `json:"comment_count"`
}

// ActivitySearchRow is an activity of the activities table, with the measures the user may show as columns
type ActivitySearchRow struct {
	ActivityRow
	// nil until computed, 0 when the records don't tell (see models.Activity)
	MovingTime     *float64 `json:"moving_time"`
	AvgMovingSpeed *float64 `json:"avg_moving_speed"`
	MaxSpeed       float64  `json:"max_speed"`
	Descent        float64  `json:"descent"`
	// 0 when not recorded
	AvgHR    uint8   `json:"avg_hr"`
	AvgPower uint16  `json:"power_avg"`
	AvgRPM   uint8   `json:"avg_rpm"`
	Calories uint16  `json:"kcal"`
	Co2      float32 `json:"co2"` // grams saved by a commute
}

// RecomputeStatus is the progress of the analysis of the activities of a device again
type RecomputeStatus struct {
	Running    bool       `json:"running"`
	Total      int        `json:"total"`  // activities to analyse
	Done       int        `json:"done"`   // analysed and saved
	Failed     int        `json:"failed"` // their FIT file is missing or can't be read
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// DeviceCount is a device of the user and how many activities it recorded
type DeviceCount struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Serial     string    `json:"serial_number"` // tells apart the units of the same model, empty when unknown
	Activities int64     `json:"activities"`
}

// DuplicateActivity is an activity of a group of duplicates, ImportedAt telling which copy came first
type DuplicateActivity struct {
	ActivityRow
	ImportedAt time.Time `json:"imported_at"`
}

// DuplicateGroup gathers activities of the same sport recorded at the same time. Identical ones start
// at the same second: the same file imported twice. The others may be the same activity recorded by
// two devices.
type DuplicateGroup struct {
	Identical  bool                `json:"identical"`
	Activities []DuplicateActivity `json:"activities"`
}

// ActivitySearch is a page of the user's activities matching a filter, with the totals of all of them
type ActivitySearch struct {
	Activities []ActivitySearchRow `json:"activities"`
	Total      int64               `json:"total"`
	Totals     PeriodTotals        `json:"totals"`
	// what the filters can offer: the user's sports, the years with activities, most recent first,
	// and the recording devices, most used first
	Sports  []string      `json:"sports"`
	Years   []int         `json:"years"`
	Devices []DeviceCount `json:"devices"`
}

type Dashboard struct {
	Totals       DashboardTotals    `json:"totals"`
	Commutes     DashboardCommutes  `json:"commutes"`
	WeekStarts   []string           `json:"week_starts"` // the Mondays of the weeks of Weekly, oldest first
	Weekly       []WeeklyVolume     `json:"weekly"`
	Sports       []SportSummary     `json:"sports"`
	Recent       []RecentActivity   `json:"recent"`
	Calendar     []ActivityCalendar `json:"calendar"` // the activities of the current month
	NbEquipments int64              `json:"nb_equipments"`
	Timezone     string             `json:"timezone"`
	// the new bests and the goals completed of the last days, the latest first: set by the caller
	Highlights []Highlight `json:"highlights"`
}

type ActivityUpload struct {
	File  *multipart.FileHeader `form:"file"`
	Path  string                `form:"path"`
	Item  uint16                `form:"item"`
	Count uint16                `form:"count"`
	// Sport is the sport of a GPX or TCX file that doesn't name it (a FIT sport name), ignored for a FIT file
	Sport string `form:"sport"`
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// type HealthDatasUpload struct {
// 	File      *multipart.FileHeader `form:"file"`
// 	Path      string                `form:"path"`
// 	Separator string                `form:"separator"`
// 	Raw       bool                  `form:"raw"`
// }

type LengthArray []Length

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// type Int8Array []int8

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// type Uint8Array []uint8

type Uint16Array []uint16

type Int16Array []int16

func (s *GpsPoint) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s GpsPoint) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *Device) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s Device) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *Length) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s Length) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *LengthArray) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s LengthArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *MultiString) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s MultiString) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan accepts NULL: the columns added after the import of an activity are NULL until computed
func (s *FloatArray) Scan(src interface{}) error {
	if src == nil {
		*s = nil
		return nil
	}
	return json.Unmarshal(src.([]byte), s)
}

func (s FloatArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan accepts NULL: the activities imported before the legs existed have none
func (s *LegArray) Scan(src interface{}) error {
	if src == nil {
		*s = nil
		return nil
	}
	return json.Unmarshal(src.([]byte), s)
}

func (s LegArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *GpsPointArray) Scan(src interface{}) error {
	if src == nil {
		*s = nil
		return nil
	}
	return json.Unmarshal(src.([]byte), s)
}

func (s GpsPointArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (s *Uint8Array) Scan(src interface{}) error {
//
// 	src = strings.Trim(src.(string), "[]")
// 	strNumbers := strings.Split(src.(string), ",")
// 	var numbers Uint8Array
//
// 	for _, strNum := range strNumbers {
// 		num, err := strconv.ParseUint(strNum, 10, 64)
// 		if err == nil {
// 			numbers = append(numbers, uint8(num))
// 		} else {
// 			return err
// 		}
// 	}
//
// 	*s = numbers
// 	return nil
// }

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (s Uint8Array) Value() (driver.Value, error) {
//
// 	var result string
//
// 	if s == nil {
// 		result = "null"
// 	} else {
// 		result = strings.Join(strings.Fields(fmt.Sprintf("%d", s)), ",")
// 	}
// 	return result, nil
// }

// Scan accepts NULL: as FloatArray
func (s *Uint16Array) Scan(src interface{}) error {
	if src == nil {
		*s = nil
		return nil
	}
	return json.Unmarshal(src.([]byte), s)
}

func (s Uint16Array) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (s *Int8Array) Scan(src interface{}) error {
// 	return json.Unmarshal(src.([]byte), s)
// }

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (s Int8Array) Value() (driver.Value, error) {
// 	return json.Marshal(s)
// }

func (s *Int16Array) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s Int16Array) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Series is a sparse series of an activity: the values and the seconds since the start of each, the
// records without a value skipped
type Series struct {
	Values     []float64 `json:"values"`
	TimeStamps []float64 `json:"time_stamps"`
}

// Add appends a value recorded offset seconds after the start
func (s *Series) Add(offset, value float64) {
	s.Values = append(s.Values, value)
	s.TimeStamps = append(s.TimeStamps, offset)
}

// Empty tells whether the series has no value
func (s *Series) Empty() bool {
	return s == nil || len(s.Values) == 0
}

// Avg returns the mean of the values, 0 without any
func (s *Series) Avg() float64 {
	if s.Empty() {
		return 0
	}
	sum := 0.0
	for _, v := range s.Values {
		sum += v
	}
	return sum / float64(len(s.Values))
}

// ActivityMetrics are the measures an activity has only with some sensors or sports, one object per
// family, absent when the recording has none of its measures
type ActivityMetrics struct {
	Running *RunningMetrics `json:"running,omitempty"`
}

// RunningMetrics are the running dynamics (HRM-Pro, HRM-Run, Running Dynamics Pod, some watches), in the
// FIT units. A measure the sensor doesn't record is absent; the averages are those of the series, so they
// follow the trimming of the activity.
type RunningMetrics struct {
	AvgVerticalOscillation float64 `json:"avg_vertical_oscillation,omitempty"` // mm
	AvgVerticalRatio       float64 `json:"avg_vertical_ratio,omitempty"`       // %
	AvgStanceTime          float64 `json:"avg_stance_time,omitempty"`          // ground contact time, ms
	AvgStanceTimePercent   float64 `json:"avg_stance_time_percent,omitempty"`  // % of the step
	AvgStanceTimeBalance   float64 `json:"avg_stance_time_balance,omitempty"`  // % of the left foot
	AvgStepLength          float64 `json:"avg_step_length,omitempty"`          // mm

	VerticalOscillations *Series `json:"vertical_oscillations,omitempty"`
	VerticalRatios       *Series `json:"vertical_ratios,omitempty"`
	StanceTimes          *Series `json:"stance_times,omitempty"`
	StanceTimePercents   *Series `json:"stance_time_percents,omitempty"`
	StanceTimeBalances   *Series `json:"stance_time_balances,omitempty"`
	StepLengths          *Series `json:"step_lengths,omitempty"`
}

// Scan accepts NULL: the activities analysed before the metrics existed have none
func (s *ActivityMetrics) Scan(src interface{}) error {
	*s = ActivityMetrics{}
	if src == nil {
		return nil
	}
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	}
	return nil
}

func (s ActivityMetrics) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// GormDataType stores the metrics as jsonb, so that SQL can read into them
func (ActivityMetrics) GormDataType() string {
	return "jsonb"
}

// WeeklyHrZones sums the seconds spent in each heart rate zone during a week (week: its Monday, YYYY-MM-DD)
type WeeklyHrZones struct {
	Week  string    `json:"week"`
	Zones []float64 `json:"zones"`
}

// The kinds of best efforts
const (
	EffortDistance = "distance" // the shortest time over a distance: Target in meters, Value in seconds
	EffortPower    = "power"    // the highest average power over a duration: Target in seconds, Value in watts
)

// BestEffort is the best stretch of an activity of a sport (the sport of its leg in a multisport one): the
// fastest over a distance, or the most powerful over a duration (measured power only)
type BestEffort struct {
	Sport       string  `json:"sport"`
	Kind        string  `json:"kind"`
	Target      float64 `json:"target"`
	Value       float64 `json:"value"`
	StartOffset float64 `json:"start_offset"` // seconds since the start of the activity
}

// BestEffortArray holds the best efforts of an activity: empty when it has none, nil until computed (stored
// as JSON null, the backfill computes them)
type BestEffortArray []BestEffort

func (s *BestEffortArray) Scan(src interface{}) error {
	*s = nil
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	}
	return nil
}

func (s BestEffortArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// GormDataType stores the efforts as jsonb, so that SQL can rank them
func (BestEffortArray) GormDataType() string {
	return "jsonb"
}

// PowerCurvePoint is the best power of a ride over a duration (s), and the second it starts at
type PowerCurvePoint struct {
	Duration    float64 `json:"duration"`
	Watts       float64 `json:"watts"`
	StartOffset float64 `json:"start_offset"` // seconds since the start of the activity
}

// PowerCurve is the power-duration curve of a ride, by increasing duration: empty without measured power,
// nil until computed (stored as JSON null)
type PowerCurve []PowerCurvePoint

func (c *PowerCurve) Scan(src interface{}) error {
	*c = nil
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	}
	return nil
}

func (c PowerCurve) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// GormDataType stores the curve as jsonb, so that SQL can take the best of each duration
func (PowerCurve) GormDataType() string {
	return "jsonb"
}

// EffortRecord is a best effort ranked among the ones of the user, with its activity
type EffortRecord struct {
	BestEffort
	Rank          int       `json:"rank"` // 1 for the record
	ActivityID    uuid.UUID `json:"activity_id"`
	ActivityTitle string    `json:"activity_title"`
	Date          time.Time `json:"date"`
}

// The kinds of highlights
const (
	HighlightEffort  = "effort"  // a best effort among the best ones of its distance or duration
	HighlightSegment = "segment" // a pass among the fastest on a segment
	HighlightGoal    = "goal"    // a goal the activity completed
)

// Highlight is what an activity achieved, compared with the activities before it: a best effort or a segment
// pass ranked in the top HighlightTop at its date, or a goal it completed
type Highlight struct {
	ActivityID    uuid.UUID `json:"activity_id"`
	ActivityTitle string    `json:"activity_title"`
	Date          time.Time `json:"date"`
	Kind          string    `json:"kind"`
	Sport         string    `json:"sport"`
	// an effort: its kind (EffortDistance, EffortPower) and target; Value: its time or power, a segment pass:
	// its time, a goal: the value of its metric at the end of the activity
	EffortKind string  `json:"effort_kind,omitempty"`
	Target     float64 `json:"target"`
	Value      float64 `json:"value"`
	// Rank at the date of the activity: 1 a new best. Previous: the best before it, nil for a goal. Current:
	// a best still the best today.
	Rank     int      `json:"rank"`
	Previous *float64 `json:"previous"`
	Current  bool     `json:"current"`
	// seconds since the start of the activity, of an effort or a pass
	StartOffset float64    `json:"start_offset"`
	SegmentID   *uuid.UUID `json:"segment_id,omitempty"`
	GoalID      *uuid.UUID `json:"goal_id,omitempty"`
	// the name of the segment or of the goal
	Name   string `json:"name,omitempty"`
	Metric string `json:"metric,omitempty"` // of a goal
}

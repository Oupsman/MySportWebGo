package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"mime/multipart"
	"strconv"
	"strings"
	"time"
)

type MultiString []string

type NotificationsBody struct {
	Message         string `json:"message"`
	Config          string `json:"config"`
	ChannelProvider string `json:"channel_provider"`
	ID              uint   `json:"id"`
}

var Genders = []string{"Male", "Female"}

var Units = []string{"Imperial", "Metric"}

type FloatArray []float64

type GpsPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type GpsPointArray []GpsPoint

type ChannelBody struct {
	ChannelID       uint64 `json:"channel_id"`
	ChannelProvider string `json:"channel_provider"`
	Config          string `json:"config"`
	Name            string `json:"name"`
	UserID          uint64 `json:"user_id"`
}

type UserBody struct {
	ID                   uint64    `json:"user_id"`
	UUID                 uuid.UUID `json:"uuid"`
	Username             string    `json:"username"`
	Password             string    `json:"newPassword"`
	PasswordConfirmation string    `json:"newPasswordConfirmation"`
	OldPassword          string    `json:"oldpassword"`
	DateOfBirth          time.Time `json:"date_of_birth"`
	Email                string    `json:"email"`
	Weight               uint16    `json:"weight"`
	Unit                 int       `json:"unit"`
	Timezone             string    `json:"timezone"`
	Gender               int       `json:"gender"`
	Height               uint16    `json:"height"`
	MaxHR                uint16    `json:"max_hr"`
	WeightObjective      uint16    `json:"weight_objective"`
	SecurityDistance     uint16    `json:"security_distance"`
	HomeTown             string    `json:"home_town"`
}

type LoginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RegisterBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
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
}

// RouteActivity is an activity of a route, with what is needed to compare it to the others
type RouteActivity struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Date     time.Time `json:"date"`
	Distance float64   `json:"distance"`
	Duration uint32    `json:"duration"`
	AvgSpeed float64   `json:"avg_speed"`
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
}

// DeviceCount is a device of the user and how many activities it recorded
type DeviceCount struct {
	Name       string `json:"name"`
	Activities int64  `json:"activities"`
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
	Activities []ActivityRow `json:"activities"`
	Total      int64         `json:"total"`
	Totals     PeriodTotals  `json:"totals"`
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
}

type ActivityUpload struct {
	File  *multipart.FileHeader `form:"file"`
	Path  string                `form:"path"`
	Item  uint16                `form:"item"`
	Count uint16                `form:"count"`
}

type HealthDatasUpload struct {
	File      *multipart.FileHeader `form:"file"`
	Path      string                `form:"path"`
	Separator string                `form:"separator"`
	Raw       bool                  `form:"raw"`
}

type LengthArray []Length

type Int8Array []int8

type Uint8Array []uint8

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

func (s *FloatArray) Scan(src interface{}) error {
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

func (s *Uint8Array) Scan(src interface{}) error {

	src = strings.Trim(src.(string), "[]")
	strNumbers := strings.Split(src.(string), ",")
	var numbers Uint8Array

	for _, strNum := range strNumbers {
		num, err := strconv.ParseUint(strNum, 10, 64)
		if err == nil {
			numbers = append(numbers, uint8(num))
		} else {
			return err
		}
	}

	*s = numbers
	return nil
}

func (s Uint8Array) Value() (driver.Value, error) {

	var result string

	if s == nil {
		result = "null"
	} else {
		result = strings.Join(strings.Fields(fmt.Sprintf("%d", s)), ",")
	}
	return result, nil
}

func (s *Uint16Array) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s Uint16Array) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *Int8Array) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s Int8Array) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *Int16Array) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s Int16Array) Value() (driver.Value, error) {
	return json.Marshal(s)
}

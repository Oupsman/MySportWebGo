package types

import (
	"database/sql/driver"
	"encoding/json"
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
	ID               uint64  `json:"user_id"`
	Username         string  `json:"username"`
	Password         string  `json:"password"`
	OldPassword      string  `json:"oldpassword"`
	Email            string  `json:"email"`
	Weight           uint16  `json:"weight"`
	Unit             int     `json:"unit"`
	Timezone         string  `json:"timezone"`
	Gender           int     `json:"gender"`
	Height           float32 `json:"height"`
	MaxHR            int     `json:"max_hr"`
	WeightObjective  int     `json:"weight_objective"`
	SecurityDistance int     `json:"security_distance"`
}

type Device struct {
	Model  string `json:"model"`
	Brand  string `json:"brand"`
	Serial string `json:"serial_number"`
}

type Length struct {
	Swolf     uint16  `json:"swolf"`
	Pace      float64 `json:"pace"`
	Length    float64 `json:"length"`
	Strokes   uint16  `json:"strokes"`
	Duration  float64 `json:"duration"`
	TimeStamp float64 `json:"timeStamp"`
}

type LengthArray []Length

type Int8Array []int8

type Uint8Array []uint8

type Uint16Array []uint16

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

func (s *GpsPointArray) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s GpsPointArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *Uint8Array) Scan(src interface{}) error {
	return json.Unmarshal(src.([]byte), s)
}

func (s Uint8Array) Value() (driver.Value, error) {
	return json.Marshal(s)
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

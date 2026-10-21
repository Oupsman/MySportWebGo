package models

import (
	"gorm.io/gorm"
	"time"
)

type HealthData struct {
	gorm.Model `json:"-"`
	User       Users     `json:"-"`
	UserID     uint      `json:"-"`
	Date       time.Time `json:"date"`
	Weight     float64   `json:"weight"`
	Fat        float64   `json:"fat"`        // percentage
	Muscle     float64   `json:"muscle"`     // percentage
	Bone       float64   `json:"bone"`       // kg
	BodyWater  float64   `json:"body_water"` // percentage
	RestHR     uint16    `json:"rest_hr"`
	Vo2Max     uint16    `json:"vo2max"`
	Systolic   uint16    `json:"systolic"`
	Diastolic  uint16    `json:"diastolic"`
}

func (db *DB) CreateHealthData(healthData *HealthData) error {
	return db.Omit("User").Create(healthData).Error
}

func (db *DB) GetHealthDatas(userID uint) ([]HealthData, error) {
	healthDatas := []HealthData{}
	err := db.Where("user_id = ?", userID).Order("date desc").Find(&healthDatas).Error
	if err != nil {
		return nil, err
	}
	return healthDatas, nil
}

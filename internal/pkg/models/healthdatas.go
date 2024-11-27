package models

import (
	"gorm.io/gorm"
	"time"
)

type HealthData struct {
	gorm.Model
	User      Users
	UserID    uint
	Date      time.Time
	Weight    float64
	Fat       float64
	Muscle    float64
	Bone      float64
	BodyWater float64
	Resthr    uint16
	Vo2max    uint16
}

func (db *DB) CreateHealthData(healthData HealthData) error {
	return db.Create(healthData).Error
}

func (db *DB) GetHealthDatas(userID uint) (HealthData, error) {
	var healthData HealthData

	return healthData, nil
}

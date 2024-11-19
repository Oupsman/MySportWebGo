package models

import (
	"gorm.io/gorm"
	"time"
)

type HealthData struct {
	gorm.Model
	User     Users
	UserID   uint
	Date     time.Time
	Weight   float64
	fatpc    float64
	musclepc float64
	resthr   uint16
	vo2max   uint16
}

func (db *DB) CreateHealthData(healthData HealthData) error {
	return db.Create(healthData).Error
}

func (db *DB) GetHealthDatas(userID uint) (HealthData, error) {
	var healthData HealthData

	return healthData, nil
}

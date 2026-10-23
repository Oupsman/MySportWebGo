package models

import (
	"errors"
	"gorm.io/gorm"
	"time"
)

// HealthData is a measure of a user, 0 for the values not measured
type HealthData struct {
	// the columns of gorm.Model, with the id visible to edit the measure
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	User      Users          `json:"-"`
	UserID    uint           `json:"-"`
	Date      time.Time      `json:"date"`
	Weight    float64        `json:"weight"`     // kg
	Fat       float64        `json:"fat"`        // percentage
	Muscle    float64        `json:"muscle"`     // percentage
	Bone      float64        `json:"bone"`       // kg
	BodyWater float64        `json:"body_water"` // percentage
	RestHR    uint16         `json:"rest_hr"`    // bpm
	Vo2Max    uint16         `json:"vo2max"`     // ml/kg/min
	Systolic  uint16         `json:"systolic"`   // mmHg
	Diastolic uint16         `json:"diastolic"`  // mmHg
}

// Validate refuses a measure without date or value, or with values no body has
func (h HealthData) Validate() error {
	if h.Date.IsZero() {
		return errors.New("missing date")
	}
	if h.Weight < 0 || h.Weight > 500 || h.Bone < 0 || h.Bone > 50 {
		return errors.New("weight and bone are in kg, between 0 and 500")
	}
	for _, percentage := range []float64{h.Fat, h.Muscle, h.BodyWater} {
		if percentage < 0 || percentage > 100 {
			return errors.New("fat, muscle and body water are percentages")
		}
	}
	if h.RestHR > 250 || h.Vo2Max > 100 || h.Systolic > 300 || h.Diastolic > 300 {
		return errors.New("heart rate, VO2max or blood pressure out of range")
	}
	if (h.Systolic == 0) != (h.Diastolic == 0) || (h.Systolic > 0 && h.Diastolic >= h.Systolic) {
		return errors.New("the blood pressure needs a systolic value above the diastolic one")
	}
	if h.Weight == 0 && h.Fat == 0 && h.Muscle == 0 && h.Bone == 0 && h.BodyWater == 0 &&
		h.RestHR == 0 && h.Vo2Max == 0 && h.Systolic == 0 {
		return errors.New("no value measured")
	}
	return nil
}

func (db *DB) CreateHealthData(healthData *HealthData) error {
	return db.Omit("User").Create(healthData).Error
}

// GetHealthData returns a measure of a user, gorm.ErrRecordNotFound for the measures of the others
func (db *DB) GetHealthData(userID uint, id uint) (HealthData, error) {
	var healthData HealthData
	err := db.Where("user_id = ? AND id = ?", userID, id).First(&healthData).Error
	return healthData, err
}

func (db *DB) UpdateHealthData(healthData *HealthData) error {
	return db.Omit("User").Save(healthData).Error
}

func (db *DB) DeleteHealthData(userID uint, id uint) error {
	return db.Where("user_id = ? AND id = ?", userID, id).Delete(&HealthData{}).Error
}

// SetWeightObjective saves the target weight of a user, 0 to remove it
func (db *DB) SetWeightObjective(userID uint, target float64) error {
	return db.Model(&Users{}).Where("id = ?", userID).Update("weight_objective", target).Error
}

func (db *DB) GetHealthDatas(userID uint) ([]HealthData, error) {
	healthDatas := []HealthData{}
	err := db.Where("user_id = ?", userID).Order("date desc").Find(&healthDatas).Error
	if err != nil {
		return nil, err
	}
	return healthDatas, nil
}

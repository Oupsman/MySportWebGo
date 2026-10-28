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

// HealthImport counts what ImportHealthDatas did with each measure
type HealthImport struct {
	Created   int `json:"created"`
	Merged    int `json:"merged"`    // values added to the measure of the same date
	Unchanged int `json:"unchanged"` // the measure of the same date already had them
}

// fillMissing sets the values of h that are 0 to the ones of other, it returns whether h changed. The
// values h already has are kept: an import never changes a measure, it completes it.
func (h *HealthData) fillMissing(other HealthData) bool {
	changed := false
	floats := []struct{ to, from *float64 }{
		{&h.Weight, &other.Weight}, {&h.Fat, &other.Fat}, {&h.Muscle, &other.Muscle}, {&h.Bone, &other.Bone},
		{&h.BodyWater, &other.BodyWater},
	}
	for _, f := range floats {
		if *f.to == 0 && *f.from != 0 {
			*f.to, changed = *f.from, true
		}
	}
	for _, f := range []struct{ to, from *uint16 }{{&h.RestHR, &other.RestHR}, {&h.Vo2Max, &other.Vo2Max}} {
		if *f.to == 0 && *f.from != 0 {
			*f.to, changed = *f.from, true
		}
	}
	// the blood pressure is a pair: both values or none
	if h.Systolic == 0 && h.Diastolic == 0 && other.Systolic != 0 {
		h.Systolic, h.Diastolic, changed = other.Systolic, other.Diastolic, true
	}
	return changed
}

// ImportHealthDatas saves valid measures of a user, in a transaction: a measure at the date of another one
// (imported before, or earlier in measures) completes it instead of duplicating it, importing the same
// measures again changes nothing.
func (db *DB) ImportHealthDatas(userID uint, measures []HealthData) (HealthImport, error) {
	var result HealthImport
	err := db.Transaction(func(tx *gorm.DB) error {
		dates := make([]time.Time, len(measures))
		for i := range measures {
			// the precision of the database: the same date found again
			measures[i].Date = measures[i].Date.Truncate(time.Microsecond)
			dates[i] = measures[i].Date
		}
		var existing []HealthData
		if err := tx.Where("user_id = ? AND date IN ?", userID, dates).Find(&existing).Error; err != nil {
			return err
		}
		byDate := map[int64]*HealthData{}
		for i := range existing {
			byDate[existing[i].Date.UnixMicro()] = &existing[i]
		}
		for _, m := range measures {
			m.ID, m.UserID = 0, userID
			if found, ok := byDate[m.Date.UnixMicro()]; ok {
				if !found.fillMissing(m) {
					result.Unchanged++
					continue
				}
				if err := tx.Omit("User").Save(found).Error; err != nil {
					return err
				}
				result.Merged++
				continue
			}
			if err := tx.Omit("User").Create(&m).Error; err != nil {
				return err
			}
			byDate[m.Date.UnixMicro()] = &m
			result.Created++
		}
		return nil
	})
	return result, err
}

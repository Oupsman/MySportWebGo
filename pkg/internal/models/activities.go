package models

import (
	"gorm.io/gorm"
	"time"
)

type Activity struct {
	gorm.Model
	User         Users
	UserID       uint      `json:"user_id"`
	Title        string    `json:"title"`
	Date         time.Time `json:"date"`
	Filename     string    `json:"filename"`
	FilePath     string    `json:"file_path"`
	Sport        string    `json:"sport"`
	IsCommute    string    `json:"is_commute"`
	Co2          int       `json:"co2"`
	Device       string    `json:"device"`
	Distance     float64   `json:"distance"`
	Duration     int       `json:"duration"`
	AvgSpeed     float64   `json:"avg_speed"`
	AvgRPM       float64   `json:"avg_rpm"`
	AvgHR        float64   `json:"avg_hr"`
	Kcal         float64   `json:"kcal"`
	Timezone     string    `json:"timezone" gorm:"default:'Europe/Paris'"`
	Masked       bool      `json:"masked" gorm:"default:true"`
	Equipment    Equipments
	EquipmentID  int    `json:"equipment_id"`
	Serialnumber string `json:"serialnumber"`
	Lastimport   bool   `json:"lastimport"`
	Visibility   int    `json:"visibility" gorm:"default:0"`
	CanComments  bool   `json:"can_comments" gorm:"default:false"`
}

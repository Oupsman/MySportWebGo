package models

import (
	"gorm.io/gorm"
	"time"
)

type Equipments struct {
	gorm.Model
	Name                string    `json:"name"`
	Brand               string    `json:"brand"`
	EquipmentModel      string    `json:"model"`
	DateOfPurchase      time.Time `json:"date_of_purchase"`
	InitialMileage      int       `json:"initial_mileage"`
	Mileage             int       `json:"mileage"`
	Weight              int       `json:"weight"`
	User                Users
	UserID              int
	MaintenanceInterval int  `json:"maintenance_interval"`
	IsDefault           bool `json:"is_default"`
}

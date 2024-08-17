package models

import "gorm.io/gorm"

type Comments struct {
	gorm.Association
	Comment    string `json:"comment"`
	UserID     int    `json:"user_id"`
	ActivityID int    `json:"Activity_id"`
	Pending    bool   `json:"pending"`
}

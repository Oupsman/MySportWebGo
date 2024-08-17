package models

import "gorm.io/gorm"

type Actions struct {
	gorm.Model
	UserID  int    `json:"user_id"`
	Message string `json:"message"`
	IsAdmin bool   `json:"is_admin"`
}

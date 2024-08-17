package models

import (
	"gorm.io/gorm"
)

type Notifications struct {
	gorm.Model
	User    Users
	UserID  uint   `json:"user_id"`
	Message string `json:"message"`
	Sent    bool   `json:"sent"`
}

type Keys struct {
	gorm.Model
	PubKey  string
	PrivKey string
}

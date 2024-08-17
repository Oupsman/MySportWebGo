package models

import "gorm.io/gorm"

type Follows struct {
	gorm.Model
	UserID   int `json:"user_id"`
	Follower int `json:"follower"`
}

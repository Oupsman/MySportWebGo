package models

import "gorm.io/gorm"

type Medias struct {
	gorm.Model
	ActivityID    int    `json:"activity_id"`
	MediaFilePath string `json:"media"`
}

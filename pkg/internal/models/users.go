package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

type Users struct {
	gorm.Model
	UUID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	Username         string    `gorm:"unique" json:"username"`
	Password         string    `json:"password"`
	Role             string    `json:"role"`
	Email            string    `json:"email" gorm:"unique"`
	DateOfBirth      time.Time `json:"date_of_birth"`
	Weight           float32   `json:"weight"`
	Unit             int       `json:"unit"`
	Timezone         string    `json:"timezone" gorm:"default:'Europe/Paris'"`
	Gender           int       `json:"gender"`
	Height           float32   `json:"height"`
	MaxHR            int       `json:"max_hr" gorm:"default:170"`
	WeightObjective  int       `json:"weight_objective" gorm:"default:0"`
	SecurityDistance int       `json:"security_distance" gorm:"default:500"`
}

type Validations struct {
	ID        uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time `sql:"index"`
	User      Users
	UserID    int
	Validated bool `json:"validated"`
}

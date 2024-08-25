package models

import (
	"MySportWeb/internal/pkg/types"
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
	Weight           int       `json:"weight"`
	Unit             int       `json:"unit"`
	Timezone         string    `json:"timezone" gorm:"default:'Europe/Paris'"`
	Gender           int       `json:"gender" gorm:"default:0"`
	Height           int       `json:"height"`
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

func (db *DB) GetUser(userID uint64) (Users, error) {
	var user Users
	result := db.First(&user, userID)
	if result.Error != nil {
		return Users{}, result.Error
	}
	return user, nil
}

func (db *DB) GetUserByUUID(uuid uuid.UUID) (Users, error) {
	var user Users
	result := db.Where("uuid = ?", uuid).First(&user)
	if result.Error != nil {
		return Users{}, result.Error
	}
	return user, nil
}

func (db *DB) CreateUser(user types.UserBody) error {
	var newUser = Users{
		Username: user.Username,
		Password: user.Password,
		Email:    user.Email,
	}
	result := db.Create(&newUser)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (db *DB) UpdateUser(user Users) error {

	result := db.Save(user)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (db *DB) GetAllUsers() ([]Users, error) {
	var users []Users
	result := db.Find(&users)
	if result.Error != nil {
		return nil, result.Error
	}
	return users, nil
}

func (user *Users) GetAge(date time.Time) int {
	age := date.Year() - user.DateOfBirth.Year()
	if date.Month() < user.DateOfBirth.Month() && date.Day() < user.DateOfBirth.Day() {
		age--
	}
	return age
}

func (user *Users) GetWeight() int {
	if user.Weight == 0 {
		return 70
	} else {
		return user.Weight
	}
}

func (user *Users) GetMaxHR(date time.Time) int {
	// A rought estimate can be made with 220-age for men and 226-age for women
	if user.MaxHR == 0 {
		return 220 + user.Gender*6 - user.GetAge(date)
	}

	return user.MaxHR
}

func (user *Users) GetGender() int {
	return user.Gender
}

func (user *Users) EstimateFTP(date time.Time) float64 {
	age := user.GetAge(date)
	if age > 35 {
		return float64(user.GetWeight())*4.40924 - float64(age-35)*0.005
	}
	return float64(user.GetWeight()) * 2.20462
}

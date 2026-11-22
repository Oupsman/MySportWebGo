package models

import (
	// "MySportWeb/internal/pkg/types" // used by the dead code below
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type Users struct {
	gorm.Model
	UUID             uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	Username         string    `gorm:"unique" json:"username"`
	Password         string    `json:"-"`
	Role             string    `json:"role"`
	Email            string    `json:"email" gorm:"unique"`
	DateOfBirth      time.Time `json:"date_of_birth"`
	Weight           uint16    `json:"weight" gorm:"default:70"`
	Unit             int       `json:"unit"`
	Timezone         string    `json:"timezone" gorm:"default:'Europe/Paris'"`
	Gender           int       `json:"gender" gorm:"default:0"`
	Height           uint16    `json:"height"`
	MaxHR            uint16    `json:"max_hr" gorm:"default:0"`
	FTP              uint16    `json:"ftp" gorm:"column:ftp;not null;default:0"` // W, 0: estimated from the rides
	WeightObjective  float64   `json:"weight_objective" gorm:"default:0"`        // the target weight, kg, 0 without
	SecurityDistance uint16    `json:"security_distance" gorm:"default:500"`
	HomeTown         string    `json:"home_town" gorm:"default:'Earth'"`
	// the session tokens issued before are refused: set when the password changes
	SessionsValidAfter time.Time `json:"-"`
	// DisabledAt is when an admin disabled the account, nil when enabled: no login, no session, no device token
	DisabledAt *time.Time `json:"disabled_at"`
	// the two-factor authentication: see models.EnableTOTP
	TOTPSecret    string     `json:"-" gorm:"column:totp_secret;not null;default:''"`
	TOTPEnabledAt *time.Time `json:"two_factor_enabled_at" gorm:"column:totp_enabled_at"`
	TOTPLastStep  int64      `json:"-" gorm:"column:totp_last_step;not null;default:0"`
	TOTPRecovery  string     `json:"-" gorm:"column:totp_recovery;not null;default:''"`
}

// RevokedSessions are the session tokens logged out before they expire, by their id (jti). They are
// forgotten once expired: the token is refused anyway.
type RevokedSessions struct {
	ID        string    `gorm:"primaryKey"`
	ExpiresAt time.Time `gorm:"index;not null"`
}

// RevokeSession refuses a session token from now on, and forgets the revoked tokens expired since
func (db *DB) RevokeSession(id string, expiresAt time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", time.Now()).Delete(&RevokedSessions{}).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&RevokedSessions{ID: id, ExpiresAt: expiresAt}).Error
	})
}

// IsSessionRevoked tells whether a session token was logged out
func (db *DB) IsSessionRevoked(id string) (bool, error) {
	var count int64
	err := db.Model(&RevokedSessions{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}

type Validations struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
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

// ErrUsernameTaken and ErrEmailTaken are returned by RegisterUser when another user has them
var (
	ErrUsernameTaken = errors.New("this username is already used")
	ErrEmailTaken    = errors.New("this email is already used")
	// ErrAdminExists refuses the setup registration of an instance which has its admin
	ErrAdminExists = errors.New("this instance already has an admin")
	// ErrInvalidInvitation is an invitation unknown, revoked, used, expired, or for another email: the
	// same answer for all, it doesn't tell which codes exist
	ErrInvalidInvitation = errors.New("invalid or expired invitation")
)

// The roles of the users
const (
	RoleAdmin = "Admin"
	RoleUser  = "User"
)

const advisoryLockRegistration = 2

// Grant is what allows a registration, besides open registrations: the setup of the first admin, or an
// invitation (the hash of its code)
type Grant struct {
	Admin          bool
	InvitationHash string
}

// RegisterUser saves a new user, whose password is already hashed: an admin with grant.Admin while the
// instance has none, a user otherwise, using up the invitation of grant.InvitationHash when set. The
// registrations are serialized: two can't both become the first admin nor use the same invitation. A deleted
// user keeps its username and email (they stay in the table).
func (db *DB) RegisterUser(user *Users, grant Grant) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?::int, 0)", advisoryLockRegistration).Error; err != nil {
			return err
		}
		user.Role = RoleUser
		if grant.Admin {
			hasAdmin, err := hasAdmin(tx)
			if err != nil {
				return err
			}
			if hasAdmin {
				return ErrAdminExists
			}
			user.Role = RoleAdmin
		}
		var invitation Invitations
		if grant.InvitationHash != "" {
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("hash = ? AND used_at IS NULL AND expires_at > ?", grant.InvitationHash, time.Now()).
				First(&invitation).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidInvitation
			}
			if err != nil {
				return err
			}
			if invitation.Email != "" && !strings.EqualFold(invitation.Email, user.Email) {
				return ErrInvalidInvitation
			}
		}
		var taken int64
		if err := tx.Unscoped().Model(&Users{}).Where("username = ?", user.Username).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrUsernameTaken
		}
		if err := tx.Unscoped().Model(&Users{}).Where("lower(email) = lower(?)", user.Email).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrEmailTaken
		}
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		if grant.InvitationHash != "" {
			return tx.Model(&Invitations{}).Where("id = ?", invitation.ID).
				Updates(map[string]interface{}{"used_at": time.Now(), "used_by_id": user.ID}).Error
		}
		return nil
	})
}

func hasAdmin(tx *gorm.DB) (bool, error) {
	var admins int64
	err := tx.Model(&Users{}).Where("role = ?", RoleAdmin).Count(&admins).Error
	return admins > 0, err
}

// HasAdmin tells whether the instance has an admin (a deleted one doesn't count): without, the setup code
// creates one
func (db *DB) HasAdmin() (bool, error) {
	return hasAdmin(&db.DB)
}

// SetRole changes the role of a user, by username: false when there is no such user
func (db *DB) SetRole(username, role string) (bool, error) {
	result := db.Model(&Users{}).Where("username = ?", username).Update("role", role)
	return result.RowsAffected > 0, result.Error
}

// UpdateUserFields changes some columns of a user, the others are kept: a field the client didn't send isn't
// erased, and what another request changed meanwhile isn't overwritten. A new email must not be another
// user's one, ErrEmailTaken otherwise: checked as RegisterUser does, with the registrations serialized.
func (db *DB) UpdateUserFields(id uint, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if email, ok := fields["email"]; ok {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?::int, 0)", advisoryLockRegistration).Error; err != nil {
				return err
			}
			var taken int64
			err := tx.Unscoped().Model(&Users{}).Where("lower(email) = lower(?) AND id <> ?", email, id).Count(&taken).Error
			if err != nil {
				return err
			}
			if taken > 0 {
				return ErrEmailTaken
			}
		}
		return tx.Model(&Users{}).Where("id = ?", id).Updates(fields).Error
	})
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (db *DB) CreateUser(user types.UserBody) error {
// 	var newUser = Users{
// 		Username: user.Username,
// 		Password: user.Password,
// 		Email:    user.Email,
// 	}
// 	result := db.Create(&newUser)
// 	if result.Error != nil {
// 		return result.Error
// 	}
// 	return nil
// }

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (db *DB) GetAllUsers() ([]Users, error) {
// 	var users []Users
// 	result := db.Find(&users)
// 	if result.Error != nil {
// 		return nil, result.Error
// 	}
// 	return users, nil
// }

// location returns the user's timezone, UTC when it is unknown
func (user *Users) location() *time.Location {
	if user.Timezone != "" {
		if location, err := time.LoadLocation(user.Timezone); err == nil {
			return location
		}
	}
	return time.UTC
}

// GetAge returns the age of the user on date, in years, -1 when their date of birth is unknown. It grows on
// their birthday in their timezone: the frontend saves the date of birth as a midnight there (16 May 22:00
// UTC for 17 May in Paris). Born on 29 February, the age grows on 1 March of the other years.
func (user *Users) GetAge(date time.Time) int {
	if user.DateOfBirth.IsZero() {
		return -1
	}
	location := user.location()
	birth, on := user.DateOfBirth.In(location), date.In(location)
	age := on.Year() - birth.Year()
	if on.Month() < birth.Month() || (on.Month() == birth.Month() && on.Day() < birth.Day()) {
		age--
	}
	return age
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (user *Users) GetWeight() uint16 {
// 	if user.Weight == 0 {
// 		return 70
// 	} else {
// 		return user.Weight
// 	}
// }

// maxEstimatedAge is the oldest age the maximum heart rate is estimated from: above, the date of birth is wrong
const maxEstimatedAge = 120

// GetMaxHR returns the maximum heart rate of the user on date: theirs, else estimated from their age
// (220 - age for men, 226 - age for women); 0 when it can't be, without date of birth
func (user *Users) GetMaxHR(date time.Time) uint16 {
	if user.MaxHR != 0 {
		return user.MaxHR
	}
	age := user.GetAge(date)
	if age < 0 || age > maxEstimatedAge {
		return 0
	}
	return uint16(220 + user.Gender*6 - age)
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (user *Users) GetGender() int {
// 	return user.Gender
// }

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (user *Users) EstimateFTP(date time.Time) float64 {
// 	age := user.GetAge(date)
// 	if age > 35 {
// 		return float64(user.GetWeight())*4.40924 - float64(age-35)*0.005
// 	}
// 	return float64(user.GetWeight()) * 2.20462
// }

// GetProfileWeights returns the weight of the profile of every user (kg), 0 when unset
func (db *DB) GetProfileWeights() (map[uint]float64, error) {
	var rows []struct {
		ID     uint
		Weight float64
	}
	if err := db.Model(&Users{}).Select("id", "weight").Scan(&rows).Error; err != nil {
		return nil, err
	}
	weights := make(map[uint]float64, len(rows))
	for _, r := range rows {
		weights[r.ID] = r.Weight
	}
	return weights, nil
}

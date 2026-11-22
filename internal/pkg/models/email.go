package models

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmailVerifications are the confirmations of email addresses sent to the users: single use, the code hashed
type EmailVerifications struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UserID    uint       `gorm:"not null" json:"-"`
	Email     string     `gorm:"not null" json:"email"`
	Hash      string     `gorm:"uniqueIndex;not null" json:"-"`
	ExpiresAt time.Time  `gorm:"not null" json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
}

// ErrInvalidVerification answers an unknown, used or expired confirmation code alike
var ErrInvalidVerification = errors.New("invalid or expired link: ask for a new one in your profile")

// CreateEmailVerification saves a confirmation of an address, the earlier unused ones of the user revoked:
// only the last address asked for can be confirmed
func (db *DB) CreateEmailVerification(v *EmailVerifications) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND used_at IS NULL", v.UserID).Delete(&EmailVerifications{}).Error; err != nil {
			return err
		}
		return tx.Create(v).Error
	})
}

// LastEmailVerification returns when the last confirmation of a user was sent, zero without
func (db *DB) LastEmailVerification(userID uint) (time.Time, error) {
	var at []time.Time
	err := db.Model(&EmailVerifications{}).Where("user_id = ?", userID).Order("created_at desc").Limit(1).
		Pluck("created_at", &at).Error
	if err != nil || len(at) == 0 {
		return time.Time{}, err
	}
	return at[0], nil
}

// PendingEmail returns the address a user changes to, not confirmed yet ("" without)
func (db *DB) PendingEmail(user Users) (string, error) {
	var emails []string
	err := db.Model(&EmailVerifications{}).
		Where("user_id = ? AND used_at IS NULL AND expires_at > ? AND lower(email) <> lower(?)", user.ID, time.Now(), user.Email).
		Order("created_at desc").Limit(1).Pluck("email", &emails).Error
	if err != nil || len(emails) == 0 {
		return "", err
	}
	return emails[0], nil
}

// UseEmailVerification confirms the address of a code (its hash): the user's address becomes it, confirmed.
// ErrInvalidVerification for an unknown, used or expired code; ErrEmailTaken when another user took the
// address meanwhile.
func (db *DB) UseEmailVerification(hash string) (Users, error) {
	var user Users
	err := db.Transaction(func(tx *gorm.DB) error {
		// the registrations and the changes of email are serialized: an address is one user's
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?::int, 0)", advisoryLockRegistration).Error; err != nil {
			return err
		}
		var v EmailVerifications
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("hash = ? AND used_at IS NULL AND expires_at > ?", hash, time.Now()).First(&v).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidVerification
		}
		if err != nil {
			return err
		}
		if err := tx.First(&user, v.UserID).Error; err != nil {
			return err
		}
		if !strings.EqualFold(v.Email, user.Email) {
			var taken int64
			if err := tx.Unscoped().Model(&Users{}).Where("lower(email) = lower(?) AND id <> ?", v.Email, user.ID).
				Count(&taken).Error; err != nil {
				return err
			}
			if taken > 0 {
				return ErrEmailTaken
			}
		}
		now := time.Now()
		if err := tx.Model(&EmailVerifications{}).Where("id = ?", v.ID).Update("used_at", now).Error; err != nil {
			return err
		}
		user.Email, user.EmailVerifiedAt = v.Email, &now
		return tx.Model(&Users{}).Where("id = ?", user.ID).
			Updates(map[string]interface{}{"email": v.Email, "email_verified_at": now}).Error
	})
	return user, err
}

// FindUserByLogin returns the user of a username or of an email address (any case), nil without
func (db *DB) FindUserByLogin(login string) (*Users, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil, nil
	}
	var users []Users
	err := db.Where("username = ? OR lower(email) = lower(?)", login, login).Limit(2).Find(&users).Error
	if err != nil || len(users) != 1 {
		return nil, err
	}
	return &users[0], nil
}

// EmailTaken tells whether another user than userID has an address (any case)
func (db *DB) EmailTaken(email string, userID uint) (bool, error) {
	var taken int64
	err := db.Unscoped().Model(&Users{}).Where("lower(email) = lower(?) AND id <> ?", email, userID).Count(&taken).Error
	return taken > 0, err
}

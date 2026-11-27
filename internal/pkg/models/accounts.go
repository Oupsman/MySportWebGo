package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrLastAdmin refuses to delete, disable or demote the last admin of the instance: nobody could manage it
var ErrLastAdmin = errors.New("the last admin of the instance can't be removed: promote another one first")

// isLastAdmin tells whether a user is the only admin, the admins locked until the transaction ends
func isLastAdmin(tx *gorm.DB, user Users) (bool, error) {
	if user.Role != RoleAdmin {
		return false, nil
	}
	var admins []Users
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("role = ?", RoleAdmin).Find(&admins).Error
	return len(admins) <= 1, err
}

// DeleteUser deletes a user and everything they own: activities (their passes on the segments), routes,
// segments, equipments with their services and default sports, devices, device tokens, push subscriptions
// and their notifications, health data, validations, imports. Their invitations stay, without them. Their files
// (MEDIA/<uuid>) are the caller's: activityService.DeleteUser. ErrLastAdmin for the last admin.
func (db *DB) DeleteUser(id uint) (Users, error) {
	var user Users
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().First(&user, id).Error; err != nil {
			return err
		}
		last, err := isLastAdmin(tx, user)
		if err != nil {
			return err
		}
		if last {
			return ErrLastAdmin
		}
		equipments := tx.Unscoped().Model(&Equipments{}).Select("id").Where("user_id = ?", id)
		channels := tx.Unscoped().Model(&Channel{}).Select("id").Where("user_id = ?", id)
		steps := []*gorm.DB{
			// the passes go with the activities and the segments (ON DELETE CASCADE)
			tx.Unscoped().Where("user_id = ?", id).Delete(&Segment{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&Activity{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&Route{}),
			tx.Unscoped().Where("equipment_id IN (?)", equipments).Delete(&Services{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&EquipmentDefault{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&Equipments{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&Devices{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&DeviceTokens{}),
			tx.Unscoped().Where("channel_id IN (?)", channels).Delete(&Notifications{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&Channel{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&HealthData{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&Validations{}),
			tx.Unscoped().Where("user_id = ?", id).Delete(&AccountImport{}),
			// the invitations: ON DELETE SET NULL
			tx.Unscoped().Delete(&Users{}, id),
		}
		for _, step := range steps {
			if step.Error != nil {
				return step.Error
			}
		}
		return nil
	})
	return user, err
}

// SetUserDisabled disables a user (now) or enables them again; false when there is no such user.
// ErrLastAdmin when disabling the last admin.
func (db *DB) SetUserDisabled(id uint, disabled bool) (bool, error) {
	found := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var user Users
		if err := tx.First(&user, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		found = true
		var at *time.Time
		if disabled {
			last, err := isLastAdmin(tx, user)
			if err != nil {
				return err
			}
			if last {
				return ErrLastAdmin
			}
			now := time.Now()
			at = &now
		}
		return tx.Model(&Users{}).Where("id = ?", id).Update("disabled_at", at).Error
	})
	return found, err
}

// SetUserRole changes the role of a user, by id: false when there is no such user. ErrLastAdmin when
// demoting the last admin.
func (db *DB) SetUserRole(id uint, role string) (bool, error) {
	found := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var user Users
		if err := tx.First(&user, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		found = true
		if role != RoleAdmin {
			last, err := isLastAdmin(tx, user)
			if err != nil {
				return err
			}
			if last {
				return ErrLastAdmin
			}
		}
		return tx.Model(&Users{}).Where("id = ?", id).Update("role", role).Error
	})
	return found, err
}

// UserSummary is a user as the admins see them
type UserSummary struct {
	ID           uint       `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	Role         string     `json:"role"`
	CreatedAt    time.Time  `json:"created_at"`
	DisabledAt   *time.Time `json:"disabled_at"`
	TwoFactor    bool       `json:"two_factor"`
	Activities   int64      `json:"activities"`
	LastActivity *time.Time `json:"last_activity"` // the date of their latest activity
	// LastDeviceUse is the last use of one of their device tokens (the desktop application)
	LastDeviceUse *time.Time `json:"last_device_use"`
	// LastLogin is their latest login of the last EventRetention, nil without
	LastLogin *time.Time `json:"last_login"`
	UUID      string     `json:"-"`
	DiskBytes int64      `json:"disk_bytes"` // their files, set by the caller
	// QuotaBytes is the quota an admin gave them, nil for the one of the instance; Quota the one that
	// applies, set by the caller; 0 for none
	QuotaBytes *int64 `json:"quota_bytes"`
	Quota      int64  `json:"quota" gorm:"-"`
}

// GetUserSummaries returns the users with their activity, by username
func (db *DB) GetUserSummaries() ([]UserSummary, error) {
	users := []UserSummary{}
	err := db.Table("users AS u").
		Select(`u.id, u.username, u.email, u.role, u.created_at, u.disabled_at, u.uuid, u.quota_bytes,
			u.totp_enabled_at IS NOT NULL AS two_factor,
			(SELECT COUNT(*) FROM activities a WHERE a.user_id = u.id) AS activities,
			(SELECT MAX(a.date) FROM activities a WHERE a.user_id = u.id) AS last_activity,
			(SELECT MAX(t.last_used_at) FROM device_tokens t WHERE t.user_id = u.id) AS last_device_use,
			(SELECT MAX(e.created_at) FROM user_events e WHERE e.user_id = u.id AND e.kind = 'login') AS last_login`).
		Where("u.deleted_at IS NULL").Order("u.username").Scan(&users).Error
	return users, err
}

// PasswordResets are the links an admin creates for a user to choose a new password
type PasswordResets struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	UserID      uint       `gorm:"not null" json:"-"`
	CreatedByID *uint      `json:"-"`
	Hash        string     `gorm:"uniqueIndex;not null" json:"-"`
	ExpiresAt   time.Time  `gorm:"not null" json:"expires_at"`
	UsedAt      *time.Time `json:"used_at"`
}

// ErrInvalidReset answers an unknown, used or expired reset code alike
var ErrInvalidReset = errors.New("invalid or expired link: ask an admin for a new one")

// CreatePasswordReset saves a reset link, the earlier unused ones of the user revoked
func (db *DB) CreatePasswordReset(reset *PasswordResets) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND used_at IS NULL", reset.UserID).Delete(&PasswordResets{}).Error; err != nil {
			return err
		}
		return tx.Create(reset).Error
	})
}

// UsePasswordReset sets the password (hashed) of the user of a reset code (its hash), ending their sessions,
// and uses the code up. ErrInvalidReset for an unknown, used or expired code, or a disabled user.
func (db *DB) UsePasswordReset(hash, password string) (Users, error) {
	var user Users
	err := db.Transaction(func(tx *gorm.DB) error {
		var reset PasswordResets
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("hash = ? AND used_at IS NULL AND expires_at > ?", hash, time.Now()).First(&reset).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidReset
		}
		if err != nil {
			return err
		}
		if err := tx.First(&user, reset.UserID).Error; err != nil {
			return err
		}
		if user.DisabledAt != nil {
			return ErrInvalidReset
		}
		now := time.Now()
		if err := tx.Model(&PasswordResets{}).Where("id = ?", reset.ID).Update("used_at", now).Error; err != nil {
			return err
		}
		// the sessions are compared to the second: the ones of this second end too (unlike a password
		// change, no new session is issued in it)
		validAfter := now.Truncate(time.Second).Add(time.Second)
		return tx.Model(&Users{}).Where("id = ?", user.ID).
			Updates(map[string]interface{}{"password": password, "sessions_valid_after": validAfter}).Error
	})
	return user, err
}

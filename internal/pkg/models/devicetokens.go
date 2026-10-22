package models

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"time"
)

// Permission is a set of rights of a device token, one bit per right
type Permission uint16

const (
	ActivitiesRead Permission = 1 << iota
	ActivitiesCreate
	ActivitiesWrite
	ActivitiesDelete
	EquipmentsRead
	EquipmentsCreate
	EquipmentsWrite
	EquipmentsDelete
	HealthDatasRead
	HealthDatasCreate
	HealthDatasWrite
	HealthDatasDelete
)

// permissionNames are the names of the permissions in the API, in bit order
var permissionNames = []string{
	"activities:read", "activities:create", "activities:write", "activities:delete",
	"equipments:read", "equipments:create", "equipments:write", "equipments:delete",
	"healthdatas:read", "healthdatas:create", "healthdatas:write", "healthdatas:delete",
}

// Has tells whether p holds all the rights of required
func (p Permission) Has(required Permission) bool {
	return p&required == required
}

// Names returns the names of the rights of p
func (p Permission) Names() []string {
	names := []string{}
	for bit, name := range permissionNames {
		if p&(1<<bit) != 0 {
			names = append(names, name)
		}
	}
	return names
}

// ParsePermissions returns the permission of a list of names, it refuses unknown names
func ParsePermissions(names []string) (Permission, error) {
	var p Permission
	for _, name := range names {
		found := false
		for bit, known := range permissionNames {
			if name == known {
				p |= 1 << bit
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("unknown permission %q", name)
		}
	}
	return p, nil
}

func (p Permission) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.Names())
}

func (p *Permission) UnmarshalJSON(data []byte) error {
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return err
	}
	parsed, err := ParsePermissions(names)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}

// DeviceTokens are long-lived API tokens of a user, for devices and scripts.
// Only the hash of the token is stored, its value is shown once at creation.
type DeviceTokens struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	User        Users      `json:"-"`
	UserID      uint       `gorm:"index;not null" json:"-"`
	Name        string     `gorm:"not null" json:"name"`
	Hash        string     `gorm:"uniqueIndex;not null" json:"-"`
	Hint        string     `json:"hint"` // the first characters of the token, to recognize it
	Permissions Permission `gorm:"not null" json:"permissions"`
	ExpiresAt   time.Time  `gorm:"not null" json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

func (db *DB) CreateDeviceToken(token *DeviceTokens) error {
	return db.Omit("User").Create(token).Error
}

// GetDeviceTokens returns the tokens of a user, expired ones included, newest first
func (db *DB) GetDeviceTokens(userID uint) ([]DeviceTokens, error) {
	tokens := []DeviceTokens{}
	err := db.Where("user_id = ?", userID).Order("created_at desc").Find(&tokens).Error
	return tokens, err
}

func (db *DB) CountDeviceTokens(userID uint) (int64, error) {
	var count int64
	err := db.Model(&DeviceTokens{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// GetDeviceTokenByHash returns a token with its user, the user is empty when deleted
func (db *DB) GetDeviceTokenByHash(hash string) (DeviceTokens, error) {
	var token DeviceTokens
	err := db.Preload("User").Where("hash = ?", hash).First(&token).Error
	return token, err
}

// DeleteDeviceToken revokes a token of a user, it returns false when the user has no such token
func (db *DB) DeleteDeviceToken(userID uint, id uuid.UUID) (bool, error) {
	result := db.Where("user_id = ? AND id = ?", userID, id).Delete(&DeviceTokens{})
	return result.RowsAffected > 0, result.Error
}

// TouchDeviceToken records the use of a token, at most once a minute to spare writes on every request
func (db *DB) TouchDeviceToken(id uuid.UUID, now time.Time) error {
	return db.Model(&DeviceTokens{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", id, now.Add(-time.Minute)).
		Update("last_used_at", now).Error
}

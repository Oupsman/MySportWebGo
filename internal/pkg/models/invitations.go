package models

import (
	"time"

	"github.com/google/uuid"
)

// Invitations let one person register while the registrations are by invitation: created by an admin,
// used once. Only the hash of the code is stored, the code is shown once at creation.
type Invitations struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedByID *uint      `gorm:"index" json:"-"` // nil once its admin is deleted
	Hash        string     `gorm:"uniqueIndex;not null" json:"-"`
	Hint        string     `json:"hint"`  // the first characters of the code, to recognize it
	Email       string     `json:"email"` // the only email it registers, "" for any
	ExpiresAt   time.Time  `gorm:"not null" json:"expires_at"`
	UsedAt      *time.Time `json:"used_at"`
	UsedByID    *uint      `json:"-"`
}

// DeletedUserName stands for a deleted user in the invitations they created or used
const DeletedUserName = "(deleted user)"

// InvitationView is an invitation as the admins see it, with the names of its users
type InvitationView struct {
	Invitations
	CreatedBy string `json:"created_by"`
	UsedBy    string `json:"used_by"` // "" while unused
}

func (db *DB) CreateInvitation(invitation *Invitations) error {
	return db.Create(invitation).Error
}

// GetInvitations returns all the invitations, newest first
func (db *DB) GetInvitations() ([]InvitationView, error) {
	invitations := []InvitationView{}
	err := db.Table("invitations").
		Select("invitations.*, COALESCE(creator.username, ?) AS created_by, "+
			"CASE WHEN invitations.used_at IS NULL THEN '' ELSE COALESCE(used.username, ?) END AS used_by",
			DeletedUserName, DeletedUserName).
		Joins("LEFT JOIN users creator ON creator.id = invitations.created_by_id").
		Joins("LEFT JOIN users used ON used.id = invitations.used_by_id").
		Order("invitations.created_at desc").Scan(&invitations).Error
	return invitations, err
}

// RevokeInvitation deletes an unused invitation: false when there is no such invitation or it was used
// (kept: it tells who invited whom)
func (db *DB) RevokeInvitation(id uuid.UUID) (bool, error) {
	result := db.Where("id = ? AND used_at IS NULL", id).Delete(&Invitations{})
	return result.RowsAffected > 0, result.Error
}

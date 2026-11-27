package models

import (
	"time"
)

// the kinds of the user events
const (
	EventLogin           = "login"
	EventLoginFailed     = "login_failed"
	EventLoginRefused    = "login_refused" // the account is disabled
	EventLogout          = "logout"
	EventRegister        = "register"
	EventPasswordReset   = "password_reset" // with a link of an admin or a forgotten password email
	EventPasswordChanged = "password_changed"
	EventTwoFactorOn     = "two_factor_enabled"
	EventTwoFactorOff    = "two_factor_disabled"
	EventUpload          = "upload"
	EventActivityDeleted = "activity_deleted"
	EventAccountExport   = "account_export"
	EventAccountImport   = "account_import"
	EventAccountDeleted  = "account_deleted"
	EventAdminRole       = "admin_role"
	EventAdminDisabled   = "admin_disabled"
	EventAdminEnabled    = "admin_enabled"
	EventAdminResetLink  = "admin_reset_link"
	EventAdminDeleted    = "admin_deleted"
	EventAdminQuota      = "admin_quota"
)

// EventKinds are the kinds of the user events, the filter of the admins
var EventKinds = []string{EventLogin, EventLoginFailed, EventLoginRefused, EventLogout, EventRegister,
	EventPasswordReset, EventPasswordChanged, EventTwoFactorOn, EventTwoFactorOff, EventUpload, EventActivityDeleted, EventAccountExport,
	EventAccountImport, EventAccountDeleted, EventAdminRole, EventAdminDisabled, EventAdminEnabled,
	EventAdminResetLink, EventAdminDeleted, EventAdminQuota}

// EventRetention is how long the user events are kept
const EventRetention = 90 * 24 * time.Hour

// UserEvent is something a user did, or an admin did to their account: see the user_events migration
type UserEvent struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UserID    *uint     `json:"user_id"`
	Username  string    `gorm:"not null;default:''" json:"username"`
	Kind      string    `gorm:"not null" json:"kind"`
	Detail    string    `gorm:"not null;default:''" json:"detail"`
	IP        string    `gorm:"column:ip;not null;default:''" json:"ip"`
}

// RecordEvent saves a user event
func (db *DB) RecordEvent(event *UserEvent) error {
	return db.Create(event).Error
}

// EventFilter selects the user events, newest first: of a user, of a kind, before an id (the next page)
type EventFilter struct {
	UserID *uint
	Kind   string
	Before int64
	Limit  int
}

// GetEvents returns the user events of the filter, newest first
func (db *DB) GetEvents(f EventFilter) ([]UserEvent, error) {
	events := []UserEvent{}
	query := db.Model(&UserEvent{})
	if f.UserID != nil {
		query = query.Where("user_id = ?", *f.UserID)
	}
	if f.Kind != "" {
		query = query.Where("kind = ?", f.Kind)
	}
	if f.Before > 0 {
		query = query.Where("id < ?", f.Before)
	}
	err := query.Order("id DESC").Limit(f.Limit).Find(&events).Error
	return events, err
}

// DropOldEvents removes the user events older than EventRetention at now, it returns how many
func (db *DB) DropOldEvents(now time.Time) (int64, error) {
	result := db.Where("created_at < ?", now.Add(-EventRetention)).Delete(&UserEvent{})
	return result.RowsAffected, result.Error
}

// NotificationLog is a notification of a user, for the admins: when it was queued and sent, to whom, through
// which channel (push, email) and about what; never its message
type NotificationLog struct {
	ID        uint       `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	SentAt    *time.Time `json:"sent_at"`
	Sent      bool       `json:"sent"`
	Kind      string     `json:"kind"`
	Provider  string     `json:"provider"`
	UserID    uint       `json:"user_id"`
	Username  string     `json:"username"`
}

// NotificationFilter selects the notifications, newest first: of a user, of a kind, before an id
type NotificationFilter struct {
	UserID *uint
	Kind   string
	Before uint
	Limit  int
}

// GetNotificationLogs returns the notifications of the filter, newest first. The ones of a removed channel
// (a push subscription gone) are removed with it.
func (db *DB) GetNotificationLogs(f NotificationFilter) ([]NotificationLog, error) {
	logs := []NotificationLog{}
	query := db.Table("notifications AS n").
		Select("n.id, n.created_at, n.sent_at, n.sent, n.kind, c.channel_provider AS provider, c.user_id, u.username").
		Joins("JOIN channels c ON c.id = n.channel_id").
		Joins("JOIN users u ON u.id = c.user_id").
		Where("n.deleted_at IS NULL")
	if f.UserID != nil {
		query = query.Where("c.user_id = ?", *f.UserID)
	}
	if f.Kind != "" {
		query = query.Where("n.kind = ?", f.Kind)
	}
	if f.Before > 0 {
		query = query.Where("n.id < ?", f.Before)
	}
	err := query.Order("n.id DESC").Limit(f.Limit).Scan(&logs).Error
	return logs, err
}

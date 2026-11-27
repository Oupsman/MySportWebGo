package models

import (
	"MySportWeb/internal/pkg/types"
	"gorm.io/gorm"
	"time"
)

type Notifications struct {
	gorm.Model
	Channel   Channel `gorm:"foreignKey:ChannelID"`
	ChannelID uint64  `json:"channel_id"`
	Message   string  `json:"message"`
	Sent      bool    `json:"sent"`
	// Kind is what it is about (NotifyGoal...), shown to the admins with SentAt
	Kind   string     `json:"kind" gorm:"not null;default:''"`
	SentAt *time.Time `json:"sent_at"`
}

type Keys struct {
	gorm.Model
	PubKey  string
	PrivKey string
}

func (db *DB) SaveKeys(keys *Keys) error {
	result := db.Create(&keys)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (db *DB) GetKeys() (*Keys, error) {
	var keys = Keys{}
	result := db.First(&keys)
	if result.RowsAffected == 0 {
		return nil, nil
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &keys, nil
}

func (db *DB) GetUnsentNotifications() ([]types.NotificationsBody, error) {
	var notifications []types.NotificationsBody
	// result := db.Where("sent = ?", false).Find(&notifications)
	result := db.Raw("SELECT n.message, n.id, n.channel_id, c.config, c.channel_provider, c.user_id FROM notifications n "+
		"JOIN channels c ON c.id = n.channel_id WHERE n.sent = ? AND n.deleted_at IS NULL AND c.deleted_at IS NULL "+
		"ORDER BY n.id", false).Scan(&notifications)
	if result.Error != nil {
		return nil, result.Error
	}
	return notifications, nil
}

func (db *DB) MarkAsSent(notificationID uint) error {
	return db.Model(&Notifications{}).Where("id = ?", notificationID).
		Updates(map[string]interface{}{"sent": true, "sent_at": time.Now()}).Error
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func (db *DB) AddNotification(channelID uint, message string) error {
// 	notification := Notifications{
// 		ChannelID: uint64(channelID),
// 		Message:   message,
// 		Sent:      false,
// 	}
// 	result := db.Create(&notification)
// 	if result.Error != nil {
// 		return result.Error
// 	}
// 	return nil
// }

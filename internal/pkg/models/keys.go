package models

import (
	"MySportWeb/internal/pkg/types"
	"gorm.io/gorm"
)

type Notifications struct {
	gorm.Model
	Channel   Channel `gorm:"foreignKey:ChannelID"`
	ChannelID uint64  `json:"channel_id"`
	Message   string  `json:"message"`
	Sent      bool    `json:"sent"`
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
	result := db.Raw("SELECT n.message as MESSAGE, n.ID as ID, c.Config AS Config, c.channel_Provider AS Channel_Provider FROM notifications N, channels c WHERE n.sent = ? AND N.channel_id = c.ID", false).Scan(&notifications)
	if result.Error != nil {
		return nil, result.Error
	}
	return notifications, nil
}

func (db *DB) MarkAsSent(notificationID uint) error {
	var notification = Notifications{}
	result := db.Find(&notification, notificationID)
	if result.Error != nil {
		return result.Error
	}
	notification.Sent = true
	result = db.Save(&notification)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (db *DB) AddNotification(channelID uint, message string) error {
	notification := Notifications{
		ChannelID: uint64(channelID),
		Message:   message,
		Sent:      false,
	}
	result := db.Create(&notification)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

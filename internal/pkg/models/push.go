package models

import (
	"time"

	"gorm.io/gorm"
)

// PushProvider is the provider of the channels that are web push subscriptions of browsers: their config is
// the PushSubscription of the browser, as JSON
const PushProvider = "browser"

// PushChannel is a push subscription of a user, without its keys
type PushChannel struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`       // the browser, as the user's device describes it
	CreatedAt time.Time `json:"created_at"` // when the browser subscribed
	Endpoint  string    `json:"-"`
}

// SavePushChannel records the push subscription of a browser of a user, replacing the one of the same
// endpoint (the same browser subscribing again)
func (db *DB) SavePushChannel(userID uint, endpoint, config, name string) (PushChannel, error) {
	var channel Channel
	err := db.Transaction(func(tx *gorm.DB) error {
		// the same browser subscribing twice at the same time: one channel
		if err := lockUser(tx, userID); err != nil {
			return err
		}
		// Find, not First: a new subscription isn't an error to log
		found := tx.Where("user_id = ? AND channel_provider = ? AND config::jsonb->>'endpoint' = ?",
			userID, PushProvider, endpoint).Limit(1).Find(&channel)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			channel = Channel{UserID: uint64(userID), ChannelProvider: PushProvider, Config: config, Name: name}
			return tx.Omit("User").Create(&channel).Error
		}
		return tx.Model(&channel).Updates(map[string]interface{}{"config": config, "name": name}).Error
	})
	return PushChannel{ID: channel.ID, Name: channel.Name, CreatedAt: channel.CreatedAt, Endpoint: endpoint}, err
}

// GetPushChannels returns the push subscriptions of a user, the most recent first
func (db *DB) GetPushChannels(userID uint) ([]PushChannel, error) {
	channels := []PushChannel{}
	err := db.Model(&Channel{}).Select("id, name, created_at, config::jsonb->>'endpoint' AS endpoint").
		Where("user_id = ? AND channel_provider = ?", userID, PushProvider).
		Order("created_at DESC").Scan(&channels).Error
	return channels, err
}

// DeletePushChannel removes a push subscription of a user and what it still had to send; false when the user
// has no such subscription
func (db *DB) DeletePushChannel(userID uint, id uint) (bool, error) {
	var deleted int64
	err := db.Transaction(func(tx *gorm.DB) error {
		var owned int64
		err := tx.Model(&Channel{}).Where("id = ? AND user_id = ? AND channel_provider = ?", id, userID, PushProvider).
			Count(&owned).Error
		if err != nil || owned == 0 {
			return err
		}
		// its notifications first: they reference it
		if err := tx.Unscoped().Where("channel_id = ?", id).Delete(&Notifications{}).Error; err != nil {
			return err
		}
		result := tx.Unscoped().Where("id = ?", id).Delete(&Channel{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted > 0, err
}

// DeleteChannel removes a channel and what it still had to send: a push subscription the push service
// says is gone
func (db *DB) DeleteChannel(id uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// its notifications first: they reference it
		if err := tx.Unscoped().Where("channel_id = ?", id).Delete(&Notifications{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("id = ?", id).Delete(&Channel{}).Error
	})
}

// NotifyUser queues a message for each channel of a user (their push subscriptions, and their email when
// on), sent by notifications.Run; the number of channels it was queued for
func (db *DB) NotifyUser(userID uint, message string) (int, error) {
	var ids []uint
	err := db.Model(&Channel{}).Where("user_id = ? AND channel_provider IN ?", userID, notifyingProviders).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	notifications := make([]Notifications, len(ids))
	for i, id := range ids {
		notifications[i] = Notifications{ChannelID: uint64(id), Message: message}
	}
	return len(ids), db.Omit("Channel").Create(&notifications).Error
}

// DropStaleNotifications removes the notifications still unsent queued before before: a push service down
// for that long, or a subscription failing for another reason than being gone
func (db *DB) DropStaleNotifications(before time.Time) (int64, error) {
	result := db.Unscoped().Where("sent = ? AND created_at < ?", false, before).Delete(&Notifications{})
	return result.RowsAffected, result.Error
}

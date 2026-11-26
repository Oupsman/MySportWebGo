package models

import "gorm.io/gorm"

// EmailProvider is the provider of the channels sending the notifications of a user by email, to their
// confirmed address (read when sending: a changed address is followed); one per user, without config
const EmailProvider = "email"

// notifyingProviders are the providers NotifyUser queues the notifications for
var notifyingProviders = []string{PushProvider, EmailProvider}

// HasEmailChannel tells whether the notifications of a user also go by email
func (db *DB) HasEmailChannel(userID uint) (bool, error) {
	var n int64
	err := db.Model(&Channel{}).Where("user_id = ? AND channel_provider = ?", userID, EmailProvider).Count(&n).Error
	return n > 0, err
}

// lockUser makes the other transactions changing the channels of a user wait for tx: checking that a channel
// is missing then creating it isn't atomic otherwise, two requests at the same time would both create it
func lockUser(tx *gorm.DB, userID uint) error {
	return tx.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Error
}

// SetEmailChannel turns the notifications by email of a user on or off; off removes what it still had to send.
// A user has one email channel at most (channels_one_email), whatever the requests at the same time.
func (db *DB) SetEmailChannel(userID uint, on bool) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := lockUser(tx, userID); err != nil {
			return err
		}
		has, err := (&DB{DB: *tx}).HasEmailChannel(userID)
		if err != nil || has == on {
			return err
		}
		if on {
			return tx.Omit("User").Create(&Channel{UserID: uint64(userID), ChannelProvider: EmailProvider, Name: "email"}).Error
		}
		channels := tx.Model(&Channel{}).Select("id").Where("user_id = ? AND channel_provider = ?", userID, EmailProvider)
		if err := tx.Unscoped().Where("channel_id IN (?)", channels).Delete(&Notifications{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("user_id = ? AND channel_provider = ?", userID, EmailProvider).Delete(&Channel{}).Error
	})
}

// EmailRecipient is where a notification by email goes: the user, their address and whether it can be used
type EmailRecipient struct {
	Username string
	Email    string
	Verified bool
	Disabled bool
}

// GetEmailRecipient returns the address the notifications of a user go to
func (db *DB) GetEmailRecipient(userID uint) (EmailRecipient, error) {
	var user Users
	if err := db.First(&user, userID).Error; err != nil {
		return EmailRecipient{}, err
	}
	return EmailRecipient{Username: user.Username, Email: user.Email, Verified: user.EmailVerifiedAt != nil,
		Disabled: user.DisabledAt != nil}, nil
}

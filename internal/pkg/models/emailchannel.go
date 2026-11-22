package models

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

// SetEmailChannel turns the notifications by email of a user on or off; off removes what it still had to send
func (db *DB) SetEmailChannel(userID uint, on bool) error {
	has, err := db.HasEmailChannel(userID)
	if err != nil || has == on {
		return err
	}
	if on {
		return db.Omit("User").Create(&Channel{UserID: uint64(userID), ChannelProvider: EmailProvider, Name: "email"}).Error
	}
	channels := db.Model(&Channel{}).Select("id").Where("user_id = ? AND channel_provider = ?", userID, EmailProvider)
	if err := db.Unscoped().Where("channel_id IN (?)", channels).Delete(&Notifications{}).Error; err != nil {
		return err
	}
	return db.Unscoped().Where("user_id = ? AND channel_provider = ?", userID, EmailProvider).Delete(&Channel{}).Error
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

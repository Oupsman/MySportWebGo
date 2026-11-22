package models

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetPendingTOTP saves the secret (encrypted) of a two-factor setup not confirmed yet: the two-factor
// authentication is off until EnableTOTP
func (db *DB) SetPendingTOTP(userID uint, encrypted string) error {
	return db.Model(&Users{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"totp_secret": encrypted, "totp_enabled_at": nil, "totp_last_step": 0, "totp_recovery": "",
	}).Error
}

// EnableTOTP turns the two-factor authentication on, with the step of the code that confirmed it (used)
// and the hashes of the recovery codes
func (db *DB) EnableTOTP(userID uint, step int64, recovery []string) error {
	return db.Model(&Users{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"totp_enabled_at": time.Now(), "totp_last_step": step, "totp_recovery": strings.Join(recovery, ","),
	}).Error
}

// DisableTOTP turns the two-factor authentication off and forgets its secret
func (db *DB) DisableTOTP(userID uint) error {
	return db.SetPendingTOTP(userID, "")
}

// ResetTOTP turns off the two-factor authentication of a user, by username: false when there is no such
// user. For the command line, when a user lost their authenticator and their recovery codes.
func (db *DB) ResetTOTP(username string) (bool, error) {
	result := db.Model(&Users{}).Where("username = ?", username).Updates(map[string]interface{}{
		"totp_secret": "", "totp_enabled_at": nil, "totp_last_step": 0, "totp_recovery": "",
	})
	return result.RowsAffected > 0, result.Error
}

// UseTOTPStep records that the code of a step was used, false when it (or a later one) already was: a
// code can't be replayed
func (db *DB) UseTOTPStep(userID uint, step int64) (bool, error) {
	result := db.Model(&Users{}).Where("id = ? AND totp_last_step < ?", userID, step).
		Update("totp_last_step", step)
	return result.RowsAffected == 1, result.Error
}

// UseRecoveryCode uses up a recovery code of a user (its hash), false when it isn't one of theirs
func (db *DB) UseRecoveryCode(userID uint, hash string) (bool, error) {
	used := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var user Users
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "totp_recovery").
			First(&user, userID).Error; err != nil {
			return err
		}
		var left []string
		for _, h := range strings.Split(user.TOTPRecovery, ",") {
			if h == "" {
				continue
			}
			if h == hash && !used {
				used = true
				continue
			}
			left = append(left, h)
		}
		if !used {
			return nil
		}
		return tx.Model(&Users{}).Where("id = ?", userID).Update("totp_recovery", strings.Join(left, ",")).Error
	})
	return used, err
}

// SetRecoveryCodes replaces the recovery codes of a user (their hashes)
func (db *DB) SetRecoveryCodes(userID uint, recovery []string) error {
	return db.Model(&Users{}).Where("id = ?", userID).Update("totp_recovery", strings.Join(recovery, ",")).Error
}

// RecoveryCodesLeft is the number of recovery codes a user hasn't used
func (u Users) RecoveryCodesLeft() int {
	n := 0
	for _, h := range strings.Split(u.TOTPRecovery, ",") {
		if h != "" {
			n++
		}
	}
	return n
}

package apicontrollers

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// enableTOTP turns on the two-factor authentication of a user, and returns its secret
func enableTOTP(t *testing.T, db *models.DB, username string) string {
	t.Helper()
	key, err := utils.NewTOTPKey(username)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := utils.EncryptSecret(key.Secret)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Model(&models.Users{}).Where("username = ?", username).Updates(map[string]interface{}{
		"totp_secret": encrypted, "totp_enabled_at": time.Now(), "totp_last_step": 0}).Error
	if err != nil {
		t.Fatal(err)
	}
	return key.Secret
}

// nextCode returns a code of a secret not used yet by the user: the code of now, or of the next period
// when the one of now was (a code is used once, the tests use several in the same 30 s)
func nextCode(t *testing.T, db *models.DB, username, secret string) string {
	t.Helper()
	var last int64
	db.Model(&models.Users{}).Where("username = ?", username).Pluck("totp_last_step", &last)
	step := time.Now().Unix() / 30
	if last >= step {
		step = last + 1
	}
	if step > time.Now().Unix()/30+1 {
		t.Fatal("no code left in the accepted window")
	}
	code, err := totp.GenerateCode(secret, time.Unix(step*30, 0))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// loginCookieMFA logs in with the password then a code, and returns the session cookie
func (s sessionClient) loginCookieMFA(db *models.DB, username, password, secret string) *http.Cookie {
	s.t.Helper()
	code, answer, _ := s.browser(http.MethodPost, "/login", nil, `{"username": "`+username+`", "password": "`+password+`"}`)
	if code != http.StatusOK || answer["challenge"] == "" {
		s.t.Fatalf("login, first step: %d %v", code, answer)
	}
	code, answer, cookies := s.browser(http.MethodPost, "/login/2fa", nil,
		`{"challenge": "`+answer["challenge"]+`", "code": "`+nextCode(s.t, db, username, secret)+`"}`)
	if code != http.StatusOK {
		s.t.Fatalf("login, second step: %d %v", code, answer)
	}
	return s.sessionCookie(cookies)
}

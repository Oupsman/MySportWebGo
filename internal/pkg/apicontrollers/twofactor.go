package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/metrics"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TOTPHeader carries the code of the authenticator app the critical admin actions ask for, each time
const TOTPHeader = "X-TOTP-Code"

// errWrongCode answers a wrong, expired or already used code alike
var errWrongCode = errors.New("wrong or already used code")

// checkTOTP checks a code of the authenticator app of a user with their enabled two-factor authentication,
// and uses it up: the same code can't be used twice
func checkTOTP(db *models.DB, user models.Users, code string) error {
	if user.TOTPEnabledAt == nil || user.TOTPSecret == "" {
		return errors.New("the two-factor authentication isn't enabled")
	}
	secret, err := utils.DecryptSecret(user.TOTPSecret)
	if err != nil {
		return err
	}
	step, ok := utils.TOTPStep(secret, code, time.Now())
	if !ok {
		return errWrongCode
	}
	fresh, err := db.UseTOTPStep(user.ID, step)
	if err != nil {
		return err
	}
	if !fresh {
		return errWrongCode
	}
	return nil
}

// checkSecondFactor checks a code of the authenticator app, or uses up a recovery code
func checkSecondFactor(db *models.DB, user models.Users, code string) error {
	err := checkTOTP(db, user, code)
	if !errors.Is(err, errWrongCode) {
		return err
	}
	used, rerr := db.UseRecoveryCode(user.ID, utils.HashRecoveryCode(code))
	if rerr != nil {
		return rerr
	}
	if used {
		return nil
	}
	return err
}

// RequireTOTP checks the code of TOTPHeader for a critical action of the user of the request: false, with
// the answer sent, when it's missing or wrong
func RequireTOTP(c *gin.Context) bool {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	code := c.GetHeader(TOTPHeader)
	if code == "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "this action needs a code of your authenticator app",
			"code": "totp_required"})
		return false
	}
	// failures are counted as the logins': a code can't be guessed through the admin routes either
	key := "totp:" + user.UUID.String()
	if blocked, wait := loginFailures.Blocked(key); blocked {
		middlewares.TooManyRequests(c, wait)
		return false
	}
	if err := checkTOTP(&App.DB, user, code); err != nil {
		if errors.Is(err, errWrongCode) {
			loginFailures.Add(key)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "totp_invalid"})
			return false
		}
		internalError(c, err)
		return false
	}
	loginFailures.Reset(key)
	return true
}

// issueSession opens a session for a user: its token goes to the cookie only, the page never sees it
func issueSession(c *gin.Context, user models.Users, mfa bool) {
	token, session, err := utils.NewSessionTokenMFA(user.ID, user.UUID, user.Role, time.Now(), mfa)
	if err != nil {
		c.JSON(500, gin.H{"error": "could not generate token"})
		return
	}
	middlewares.SetSessionCookie(c, token, session.ExpiresAt)
	// this browser logs in during an attack on the account (see accountFailures)
	middlewares.SetKnownDeviceCookie(c, user.Username, time.Now())
	metrics.Logins.WithLabelValues("success").Inc()
	detail := "password"
	if mfa {
		detail = "password and second factor"
	}
	recordEvent(c, user, models.EventLogin, detail)
	c.JSON(200, gin.H{"session": newSessionInfo(user, session)})
}

// LoginSecondFactor is the second step of a login with the two-factor authentication: the challenge of the
// first one, and a code of the authenticator app or a recovery code
func LoginSecondFactor(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	var body struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	userUUID, err := utils.ParseChallengeToken(body.Challenge)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "the login expired, start again"})
		return
	}
	key := "2fa:" + userUUID.String()
	if blocked, wait := loginFailures.Blocked(key); blocked {
		metrics.Logins.WithLabelValues("locked").Inc()
		middlewares.TooManyRequests(c, wait)
		return
	}
	user, err := App.DB.GetUserByUUID(userUUID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "the login expired, start again"})
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if user.DisabledAt != nil {
		recordEvent(c, user, models.EventLoginRefused, "account disabled")
		c.JSON(http.StatusForbidden, gin.H{"error": "this account is disabled, ask an admin of the instance"})
		return
	}
	if err := checkSecondFactor(&App.DB, user, body.Code); err != nil {
		if errors.Is(err, errWrongCode) {
			loginFailures.Add(key)
			metrics.Logins.WithLabelValues("failure").Inc()
			recordEvent(c, user, models.EventLoginFailed, "wrong second factor code")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		internalError(c, err)
		return
	}
	loginFailures.Reset(key)
	issueSession(c, user, true)
}

// checkPassword checks the password a sensitive change of the user of the request is confirmed with:
// false, with the answer sent, when wrong
func checkPassword(c *gin.Context, password string) bool {
	if !utils.CompareHashPassword(password, middlewares.CurrentUser(c).Password) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "wrong password"})
		return false
	}
	return true
}

// TwoFactorStatus answers whether the two-factor authentication of the user is on, how many recovery codes
// they have left, and whether they must have it (the admins)
func TwoFactorStatus(c *gin.Context) {
	user := middlewares.CurrentUser(c)
	session, _ := middlewares.CurrentSession(c)
	c.JSON(http.StatusOK, gin.H{"enabled": user.TOTPEnabledAt != nil, "enabled_at": user.TOTPEnabledAt,
		"recovery_codes_left": user.RecoveryCodesLeft(), "required": user.Role == models.RoleAdmin,
		"session_verified": session.MFA})
}

// SetupTwoFactor starts the setup: a new secret, to scan in the authenticator app, confirmed by
// EnableTwoFactor. Asks the password; refused while enabled (disable it first).
func SetupTwoFactor(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	var body struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if !checkPassword(c, body.Password) {
		return
	}
	if user.TOTPEnabledAt != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "the two-factor authentication is already on"})
		return
	}
	key, err := utils.NewTOTPKey(user.Username)
	if err != nil {
		internalError(c, err)
		return
	}
	encrypted, err := utils.EncryptSecret(key.Secret)
	if err != nil {
		internalError(c, err)
		return
	}
	if err := App.DB.SetPendingTOTP(user.ID, encrypted); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"secret": key.Secret, "url": key.URL, "qr_code": key.QRCode})
}

// EnableTwoFactor confirms the setup with a code of the authenticator app: the two-factor authentication
// is on, the recovery codes are answered (once), and the session of the request becomes a verified one
func EnableTwoFactor(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	var body struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if user.TOTPEnabledAt != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "the two-factor authentication is already on"})
		return
	}
	if user.TOTPSecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start the setup first"})
		return
	}
	secret, err := utils.DecryptSecret(user.TOTPSecret)
	if err != nil {
		internalError(c, err)
		return
	}
	step, ok := utils.TOTPStep(secret, body.Code, time.Now())
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wrong code: check the time of your phone"})
		return
	}
	codes, hashes := utils.NewRecoveryCodes()
	if err := App.DB.EnableTOTP(user.ID, step, hashes); err != nil {
		internalError(c, err)
		return
	}
	// the session of the request went through the second factor now: it is replaced by a verified one
	if session, ok := middlewares.CurrentSession(c); ok {
		if err := App.DB.RevokeSession(session.ID, session.ExpiresAt); err != nil {
			App.Logger.Warn().Err(err).Msg("session before the two-factor setup not revoked")
		}
	}
	token, session, err := utils.NewSessionTokenMFA(user.ID, user.UUID, user.Role, time.Now(), true)
	if err != nil {
		internalError(c, err)
		return
	}
	middlewares.SetSessionCookie(c, token, session.ExpiresAt)
	App.Logger.Info().Uint("user_id", user.ID).Msg("two-factor authentication enabled")
	recordEvent(c, user, models.EventTwoFactorOn, "")
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes, "session": newSessionInfo(user, session)})
}

// DisableTwoFactor turns the two-factor authentication off, with the password and a code (or a recovery
// code). The admins can't: they must have it.
func DisableTwoFactor(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if user.Role == models.RoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "the admins must keep the two-factor authentication"})
		return
	}
	if !checkPassword(c, body.Password) {
		return
	}
	if user.TOTPEnabledAt != nil {
		if err := checkSecondFactor(&App.DB, user, body.Code); err != nil {
			if errors.Is(err, errWrongCode) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			internalError(c, err)
			return
		}
	}
	if err := App.DB.DisableTOTP(user.ID); err != nil {
		internalError(c, err)
		return
	}
	App.Logger.Info().Uint("user_id", user.ID).Msg("two-factor authentication disabled")
	recordEvent(c, user, models.EventTwoFactorOff, "")
	c.JSON(http.StatusOK, gin.H{"enabled": false})
}

// NewRecoveryCodes replaces the recovery codes, with the password and a code of the authenticator app
func NewRecoveryCodes(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if !checkPassword(c, body.Password) {
		return
	}
	// a wrong code, or the two-factor authentication off: both the user's to fix
	if err := checkTOTP(&App.DB, user, body.Code); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	codes, hashes := utils.NewRecoveryCodes()
	if err := App.DB.SetRecoveryCodes(user.ID, hashes); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes})
}

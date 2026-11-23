package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/services/activityService"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	// PasswordResetLifetime is how long a password reset link is valid
	PasswordResetLifetime = 24 * time.Hour
	// minPasswordLength is the shortest password a reset link sets, as the sign up page asks
	minPasswordLength = 6
)

// diskUsage returns the size of the files of a directory, 0 when it doesn't exist
func diskUsage(dir string) int64 {
	var size int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}

// GetUsers answers the users of the instance, with their activity and the size of their files
func GetUsers(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	users, err := App.DB.GetUserSummaries()
	if err != nil {
		internalError(c, err)
		return
	}
	for i := range users {
		if id, err := uuid.Parse(users[i].UUID); err == nil {
			users[i].DiskBytes = diskUsage(activityService.UserMediaDir(id))
		}
		users[i].Quota = models.Users{QuotaBytes: users[i].QuotaBytes}.Quota()
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// targetUser returns the user of the :id parameter, another than the admin of the request: their own
// account is changed from their profile (or the command line). False, with the answer sent, otherwise.
func targetUser(c *gin.Context) (models.Users, bool) {
	App := c.MustGet("App").(*app.App)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return models.Users{}, false
	}
	if uint(id) == middlewares.CurrentUser(c).ID {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "change your own account from your profile"})
		return models.Users{}, false
	}
	user, err := App.DB.GetUser(id)
	if err != nil {
		lookupError(c, err, "user")
		return models.Users{}, false
	}
	return user, true
}

// adminChangeError answers the failure of a change of a user: 409 for the last admin
func adminChangeError(c *gin.Context, err error) {
	if errors.Is(err, models.ErrLastAdmin) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	internalError(c, err)
}

// SetUserRole makes a user an admin or a user, {"role": "Admin"|"User"}, with a code of the authenticator
func SetUserRole(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	var body struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if body.Role != models.RoleAdmin && body.Role != models.RoleUser {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be Admin or User"})
		return
	}
	user, ok := targetUser(c)
	if !ok || !RequireTOTP(c) {
		return
	}
	if _, err := App.DB.SetUserRole(user.ID, body.Role); err != nil {
		adminChangeError(c, err)
		return
	}
	App.Logger.Info().Uint("user_id", user.ID).Str("role", body.Role).Uint("by", middlewares.CurrentUser(c).ID).
		Msg("role changed")
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "role": body.Role})
}

// setDisabled disables or enables a user, with a code of the authenticator
func setDisabled(c *gin.Context, disabled bool) {
	App := c.MustGet("App").(*app.App)
	user, ok := targetUser(c)
	if !ok || !RequireTOTP(c) {
		return
	}
	if _, err := App.DB.SetUserDisabled(user.ID, disabled); err != nil {
		adminChangeError(c, err)
		return
	}
	App.Logger.Info().Uint("user_id", user.ID).Bool("disabled", disabled).Uint("by", middlewares.CurrentUser(c).ID).
		Msg("account disabled or enabled")
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "disabled": disabled})
}

// DisableUser disables a user: no login, their sessions and device tokens refused; their data kept
func DisableUser(c *gin.Context) { setDisabled(c, true) }

// EnableUser enables a disabled user again
func EnableUser(c *gin.Context) { setDisabled(c, false) }

// AdminDeleteUser deletes a disabled user and everything they own, with a code of the authenticator: an
// account is disabled first, a slip can't delete it at once
func AdminDeleteUser(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user, ok := targetUser(c)
	if !ok {
		return
	}
	if user.DisabledAt == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "disable the account before deleting it"})
		return
	}
	if !RequireTOTP(c) {
		return
	}
	if _, err := activityService.DeleteUser(&App.DB, user.ID, App.Logger); err != nil {
		adminChangeError(c, err)
		return
	}
	App.Logger.Info().Uint("user_id", user.ID).Uint("by", middlewares.CurrentUser(c).ID).Msg("account deleted by an admin")
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "deleted": true})
}

// PasswordResetLink is the page of the web application (hash routes) choosing a new password with a code
func PasswordResetLink(code string) string {
	return strings.TrimRight(vars.Domain, "/") + "/#/reset-password?code=" + url.QueryEscape(code)
}

// NewPasswordReset creates the link of a single use password reset, its code kept hashed; the user's
// earlier links stop working
func NewPasswordReset(db *models.DB, userID uint, createdBy *uint) (string, models.PasswordResets, error) {
	code := utils.NewCode()
	reset := models.PasswordResets{UserID: userID, CreatedByID: createdBy, Hash: utils.HashCode(code),
		ExpiresAt: time.Now().Add(PasswordResetLifetime)}
	if err := db.CreatePasswordReset(&reset); err != nil {
		return "", reset, err
	}
	return PasswordResetLink(code), reset, nil
}

// CreatePasswordReset answers, once, a link for a user to choose a new password, with a code of the
// authenticator: there is no email, the admin gives it to them
func CreatePasswordReset(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user, ok := targetUser(c)
	if !ok || !RequireTOTP(c) {
		return
	}
	admin := middlewares.CurrentUser(c).ID
	link, reset, err := NewPasswordReset(&App.DB, user.ID, &admin)
	if err != nil {
		internalError(c, err)
		return
	}
	App.Logger.Info().Uint("user_id", user.ID).Uint("by", admin).Msg("password reset link created")
	c.JSON(http.StatusCreated, gin.H{"link": link, "expires_at": reset.ExpiresAt})
}

// ResetPassword sets a new password with the code of a reset link: the sessions of the user end, their
// two-factor authentication stays (the link doesn't replace it)
func ResetPassword(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	var body struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if len([]rune(body.Password)) < minPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the password needs at least 6 characters"})
		return
	}
	hash, err := utils.GenerateHashPassword(body.Password)
	if errors.Is(err, bcrypt.ErrPasswordTooLong) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the password is longer than 72 bytes"})
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	user, err := App.DB.UsePasswordReset(utils.HashCode(body.Code), hash)
	if errors.Is(err, models.ErrInvalidReset) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	// a new password: the failures with the old one are forgotten, from every IP
	accountFailures.Reset(strings.ToLower(user.Username))
	loginFailures.ResetPrefix(loginFailureKey(user.Username, ""))
	App.Logger.Info().Uint("user_id", user.ID).Msg("password reset")
	c.JSON(http.StatusOK, gin.H{"username": user.Username})
}

// exporting are the users whose export is running: one at a time each
var exporting sync.Map

// ExportAccount downloads the zip of everything the user owns (activityService.ExportUser), streamed: it
// isn't stored on the server. Its write deadline is lifted: a large export takes longer than WriteTimeout.
func ExportAccount(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	if _, running := exporting.LoadOrStore(user.ID, true); running {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "an export of your data is already running"})
		return
	}
	defer exporting.Delete(user.ID)
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{}); err != nil {
		App.Logger.Warn().Err(err).Msg("export: the write deadline can't be lifted")
	}
	name := fmt.Sprintf("mysportweb-%s-%s.zip", user.Username, time.Now().Format("2006-01-02"))
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Status(http.StatusOK)
	if err := activityService.ExportUser(c.Request.Context(), &App.DB, user, c.Writer); err != nil {
		// the headers are sent: the download ends broken, the zip unreadable
		App.Logger.Error().Err(err).Uint("user_id", user.ID).Msg("export failed")
		return
	}
	App.Logger.Info().Uint("user_id", user.ID).Msg("data exported")
}

// DeleteAccount deletes the account of the user and everything they own, with their password, their
// username typed again, and a code of their authenticator when they have the two-factor authentication.
// The last admin can't.
func DeleteAccount(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	var body struct {
		Password string `json:"password"`
		Username string `json:"username"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if body.Username != user.Username {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type your username to confirm"})
		return
	}
	if !checkPassword(c, body.Password) {
		return
	}
	if user.TOTPEnabledAt != nil {
		if err := checkSecondFactor(&App.DB, user, body.Code); err != nil {
			if errors.Is(err, errWrongCode) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "a code of your authenticator app is needed: " + err.Error()})
				return
			}
			internalError(c, err)
			return
		}
	}
	if _, err := activityService.DeleteUser(&App.DB, user.ID, App.Logger); err != nil {
		adminChangeError(c, err)
		return
	}
	middlewares.ClearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

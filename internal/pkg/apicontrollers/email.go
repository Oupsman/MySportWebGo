package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/mail"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// VerificationLifetime is how long the link confirming an email address is valid
	VerificationLifetime = 48 * time.Hour
	// resendInterval: a confirmation is sent again after it only
	resendInterval = 2 * time.Minute
	// maxForgotten, forgottenWindow: the password reset emails of an account
	maxForgotten    = 3
	forgottenWindow = time.Hour
	// emailTimeout bounds the sending of an email asked by a request
	emailTimeout = 30 * time.Second
)

// forgotten limits the reset emails per account: a mailbox can't be flooded through the login page
var forgotten = middlewares.NewRateLimiter(maxForgotten, forgottenWindow)

// appLink is a page of the web application (hash routes) with a code
func appLink(page, code string) string {
	return strings.TrimRight(vars.Domain, "/") + "/#/" + page + "?code=" + url.QueryEscape(code)
}

// sendVerification sends the confirmation of an address of a user (theirs, or the one they change to)
func sendVerification(ctx context.Context, App *app.App, user models.Users, email string) error {
	code := utils.NewCode()
	v := models.EmailVerifications{UserID: user.ID, Email: email, Hash: utils.HashCode(code),
		ExpiresAt: time.Now().Add(VerificationLifetime)}
	if err := App.DB.CreateEmailVerification(&v); err != nil {
		return err
	}
	return App.SendMail(ctx, "verification", mail.Verification(user.Username, email, appLink("verify-email", code),
		VerificationLifetime))
}

// sendVerificationLater sends a confirmation in the background: the request doesn't wait for the server
func sendVerificationLater(App *app.App, user models.Users, email string) {
	if !App.MailEnabled() {
		return
	}
	App.Go(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, emailTimeout)
		defer cancel()
		if err := sendVerification(ctx, App, user, email); err != nil {
			App.Logger.Warn().Err(err).Uint("user_id", user.ID).Msg("email confirmation not sent")
		}
	})
}

// VerifyEmail confirms an address with the code of its link: open without session, the link is opened from
// the mailbox
func VerifyEmail(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	var body struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	user, err := App.DB.UseEmailVerification(utils.HashCode(body.Code))
	switch {
	case errors.Is(err, models.ErrInvalidVerification):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, models.ErrEmailTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "another account uses this address now"})
	case err != nil:
		internalError(c, err)
	default:
		App.Logger.Info().Uint("user_id", user.ID).Msg("email address confirmed")
		c.JSON(http.StatusOK, gin.H{"email": user.Email})
	}
}

// EmailStatus answers the address of the user, whether it is confirmed, the one they change to (not
// confirmed yet), and whether the instance sends emails
func EmailStatus(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	pending, err := App.DB.PendingEmail(user)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"email": user.Email, "verified_at": user.EmailVerifiedAt, "pending": pending,
		"enabled": App.MailEnabled()})
}

// ResendVerification sends the confirmation again: of the address the user changes to, else of theirs
func ResendVerification(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	if !App.MailEnabled() {
		c.JSON(http.StatusConflict, gin.H{"error": app.ErrMailOff.Error()})
		return
	}
	pending, err := App.DB.PendingEmail(user)
	if err != nil {
		internalError(c, err)
		return
	}
	email := pending
	if email == "" {
		if user.EmailVerifiedAt != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "your address is already confirmed"})
			return
		}
		email = user.Email
	}
	last, err := App.DB.LastEmailVerification(user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	if wait := resendInterval - time.Since(last); wait > 0 {
		middlewares.TooManyRequests(c, wait)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), emailTimeout)
	defer cancel()
	if err := sendVerification(ctx, App, user, email); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "the email could not be sent, try again later"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent_to": email})
}

// forgotAnswer is the answer to every request of a password reset: it doesn't tell which accounts exist
const forgotAnswer = "if an account with a confirmed address matches, an email with a link is on its way"

// ForgotPassword sends a password reset link to the confirmed address of the account of a username or an
// email. The answer is the same whatever the account (none, unconfirmed, disabled, too many requests): the
// email is sent in the background, the time doesn't tell either.
func ForgotPassword(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	var body struct {
		Login string `json:"login"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if !App.MailEnabled() {
		c.JSON(http.StatusConflict, gin.H{"error": app.ErrMailOff.Error() + "; ask an admin for a reset link"})
		return
	}
	user, err := App.DB.FindUserByLogin(body.Login)
	if err != nil {
		internalError(c, err)
		return
	}
	if user != nil && user.EmailVerifiedAt != nil && user.DisabledAt == nil {
		key := "forgot:" + user.UUID.String()
		if blocked, _ := forgotten.Blocked(key); !blocked {
			forgotten.Add(key)
			target := *user
			App.Go(func(ctx context.Context) {
				link, reset, err := NewPasswordReset(&App.DB, target.ID, nil)
				if err != nil {
					App.Logger.Error().Err(err).Uint("user_id", target.ID).Msg("password reset link not created")
					return
				}
				ctx, cancel := context.WithTimeout(ctx, emailTimeout)
				defer cancel()
				_ = App.SendMail(ctx, "password_reset", mail.PasswordReset(target.Username, target.Email, link,
					time.Until(reset.ExpiresAt).Round(time.Hour)))
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": forgotAnswer})
}

// SendTestEmail sends a test email to the admin of the request: their address, confirmed or not. The error
// of the server is answered: it tells the admin what to fix.
func SendTestEmail(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	if !App.MailEnabled() {
		c.JSON(http.StatusConflict, gin.H{"error": app.ErrMailOff.Error()})
		return
	}
	host := "the SMTP server"
	if s, ok := App.Mailer.(*mail.SMTP); ok {
		host = s.Config.Host
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), emailTimeout)
	defer cancel()
	if err := App.SendMail(ctx, "test", mail.Test(user.Username, user.Email, host)); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent_to": user.Email})
}

// EmailNotifications answers whether the notifications of the user also go by email, and whether they can:
// the emails on, and their address confirmed
func EmailNotifications(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	on, err := App.DB.HasEmailChannel(user.ID)
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": on, "available": App.MailEnabled() && user.EmailVerifiedAt != nil,
		"email": user.Email, "verified": user.EmailVerifiedAt != nil})
}

// SetEmailNotifications turns the notifications by email on or off, {"enabled": true}: on needs the emails
// on and a confirmed address
func SetEmailNotifications(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	user := middlewares.CurrentUser(c)
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		badRequest(c, err)
		return
	}
	if body.Enabled {
		if !App.MailEnabled() {
			c.JSON(http.StatusConflict, gin.H{"error": app.ErrMailOff.Error()})
			return
		}
		if user.EmailVerifiedAt == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "confirm your email address first"})
			return
		}
	}
	if err := App.DB.SetEmailChannel(user.ID, body.Enabled); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": body.Enabled})
}

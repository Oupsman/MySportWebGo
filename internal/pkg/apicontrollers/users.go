package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/services/activityService"
	"errors"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// /MEDIA/user uuid/profile pictures/profile picture.png

// describeSecret returns facts about a secret that help diagnose a login failure without revealing it
func describeSecret(secret string) map[string]interface{} {
	nonASCII := false
	for _, r := range secret {
		if r > unicode.MaxASCII {
			nonASCII = true
			break
		}
	}
	return map[string]interface{}{
		"length":               utf8.RuneCountInString(secret),
		"bytes":                len(secret),
		"surrounding_spaces":   strings.TrimSpace(secret) != secret,
		"non_ascii_characters": nonASCII,
	}
}

// loginFailures counts the failed logins per username: a password can't be guessed from many IPs
const (
	maxLoginFailures   = 5
	loginFailureWindow = 15 * time.Minute
)

var loginFailures = middlewares.NewRateLimiter(maxLoginFailures, loginFailureWindow)

// invalidLogin is the answer to a wrong username or a wrong password alike: it doesn't tell which
// usernames exist
const invalidLogin = "invalid username or password"

func Login(c *gin.Context) {

	var user types.LoginBody

	App := c.MustGet("App").(*app.App)
	db := App.DB
	// never log the password nor the hash, only facts about them
	logger := App.Logger.With().Str("handler", "login").Str("origin", c.GetHeader("Origin")).
		Str("client_ip", c.ClientIP()).Logger()

	if err := c.ShouldBindJSON(&user); err != nil {
		logger.Debug().Err(err).Str("content_type", c.ContentType()).Msg("login refused: invalid request body")
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	logger = logger.With().Str("username", user.Username).Logger()
	logger.Debug().Interface("username_facts", describeSecret(user.Username)).
		Interface("password_facts", describeSecret(user.Password)).Msg("login attempt")

	failuresKey := strings.ToLower(user.Username)
	if blocked, wait := loginFailures.Blocked(failuresKey); blocked {
		logger.Warn().Msg("login refused: too many failures for this username")
		middlewares.TooManyRequests(c, wait)
		return
	}

	var existingUser models.Users

	err := db.Where("Username = ?", user.Username).First(&existingUser).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.Error().Err(err).Msg("login refused: database error while looking up the user")
		c.JSON(500, gin.H{"error": "could not check the login"})
		return
	}

	if existingUser.ID == 0 {
		logger.Debug().Msg("login refused: no user with this username (it is case sensitive, the email does not work)")
		// as long as a wrong password
		utils.CheckPasswordOfNobody(user.Password)
		loginFailures.Add(failuresKey)
		c.JSON(400, gin.H{"error": invalidLogin})
		return
	}
	logger.Debug().Uint("user_id", existingUser.ID).Int("stored_hash_length", len(existingUser.Password)).
		Bool("stored_hash_is_bcrypt", strings.HasPrefix(existingUser.Password, "$2")).Msg("user found")

	if err := utils.CheckPassword(user.Password, existingUser.Password); err != nil {
		logger.Debug().Err(err).Uint("user_id", existingUser.ID).Msg("login refused: password check failed")
		loginFailures.Add(failuresKey)
		c.JSON(400, gin.H{"error": invalidLogin})
		return
	}
	loginFailures.Reset(failuresKey)
	logger.Debug().Uint("user_id", existingUser.ID).Msg("password ok, issuing token")

	tokenString, _, err := utils.NewSessionToken(existingUser.ID, existingUser.UUID, existingUser.Role, time.Now())
	if err != nil {
		c.JSON(500, gin.H{"error": "could not generate token"})
		return
	}
	c.SetCookie("mysportweb_session", tokenString, 3600, "/", "localhost", false, true)
	c.JSON(200, gin.H{"token": tokenString})
}

func Register(c *gin.Context) {

	var body types.RegisterBody
	App := c.MustGet("App").(*app.App)
	db := App.DB

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	user := models.Users{Username: strings.TrimSpace(body.Username), Email: strings.TrimSpace(body.Email)}

	if user.Username == "" || body.Password == "" || user.Email == "" {
		c.JSON(400, gin.H{"error": "missing required fields"})
		return
	}
	if address, err := mail.ParseAddress(user.Email); err != nil || address.Address != user.Email {
		c.JSON(400, gin.H{"error": "invalid email"})
		return
	}

	hash, err := utils.GenerateHashPassword(body.Password)
	if errors.Is(err, bcrypt.ErrPasswordTooLong) {
		c.JSON(400, gin.H{"error": "the password is longer than 72 bytes"})
		return
	}
	if err != nil {
		App.Logger.Error().Err(err).Msg("registration failed: password not hashed")
		c.JSON(500, gin.H{"error": "could not generate password hash"})
		return
	}
	user.Password = hash

	err = db.RegisterUser(&user)
	if errors.Is(err, models.ErrUsernameTaken) || errors.Is(err, models.ErrEmailTaken) {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		App.Logger.Error().Err(err).Str("username", user.Username).Msg("registration failed")
		c.JSON(500, gin.H{"error": "could not register the user"})
		return
	}

	c.JSON(201, gin.H{"success": "user registered"})
}

// Logout revokes the session token of the request: it is refused from now on, even if it was copied
func Logout(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	session, ok := middlewares.CurrentSession(c)
	if !ok {
		c.JSON(400, gin.H{"error": "not a session token"})
		return
	}
	if err := db.RevokeSession(session.ID, session.ExpiresAt); err != nil {
		c.JSON(500, gin.H{"error": "could not log out"})
		return
	}
	c.JSON(200, gin.H{"success": "user logged out"})
}

func GetUser(c *gin.Context) {
	c.JSON(200, middlewares.CurrentUser(c))
}

// userUpdate is what the user can change on their account. A field left out (or null) keeps its value.
type userUpdate struct {
	OldPassword          string     `json:"oldpassword"`
	Password             string     `json:"newPassword"`
	PasswordConfirmation string     `json:"newPasswordConfirmation"`
	Email                *string    `json:"email"`
	DateOfBirth          *time.Time `json:"date_of_birth"`
	Weight               *uint16    `json:"weight"`
	Height               *uint16    `json:"height"`
	MaxHR                *uint16    `json:"max_hr"`
	Gender               *int       `json:"gender"`
	Unit                 *int       `json:"unit"`
	SecurityDistance     *uint16    `json:"security_distance"`
	HomeTown             *string    `json:"home_town"`
	Timezone             *string    `json:"timezone"`
}

// the bounds of the values of userUpdate
const (
	maxWeight      = 500 // kg
	maxHeight      = 300 // cm
	maxHeartRate   = 250 // bpm, 0 to estimate it from the age
	maxHomeTownLen = 100
)

// fields returns the columns to change, or why the update is refused
func (u userUpdate) fields(now time.Time) (map[string]interface{}, error) {
	fields := map[string]interface{}{}
	if u.Email != nil {
		email := strings.TrimSpace(*u.Email)
		if address, err := mail.ParseAddress(email); err != nil || address.Address != email {
			return nil, errors.New("invalid email")
		}
		fields["email"] = email
	}
	if u.DateOfBirth != nil {
		// the zero date is an unknown one: the maximum heart rate isn't estimated from it
		if u.DateOfBirth.After(now) {
			return nil, errors.New("the date of birth is in the future")
		}
		fields["date_of_birth"] = *u.DateOfBirth
	}
	if u.Weight != nil {
		if *u.Weight > maxWeight {
			return nil, errors.New("the weight is in kg, at most 500")
		}
		fields["weight"] = *u.Weight
	}
	if u.Height != nil {
		if *u.Height > maxHeight {
			return nil, errors.New("the height is in cm, at most 300")
		}
		fields["height"] = *u.Height
	}
	if u.MaxHR != nil {
		if *u.MaxHR > maxHeartRate {
			return nil, errors.New("the maximum heart rate is at most 250, 0 to estimate it")
		}
		fields["max_hr"] = *u.MaxHR
	}
	if u.Gender != nil {
		if *u.Gender < 0 || *u.Gender >= len(types.Genders) {
			return nil, errors.New("unknown gender")
		}
		fields["gender"] = *u.Gender
	}
	if u.Unit != nil {
		if *u.Unit < 0 || *u.Unit >= len(types.Units) {
			return nil, errors.New("unknown unit")
		}
		fields["unit"] = *u.Unit
	}
	if u.SecurityDistance != nil {
		// 0 is the user's choice: no privacy zone
		fields["security_distance"] = *u.SecurityDistance
	}
	if u.HomeTown != nil {
		homeTown := strings.TrimSpace(*u.HomeTown)
		if utf8.RuneCountInString(homeTown) > maxHomeTownLen {
			return nil, errors.New("the home town is at most 100 characters")
		}
		fields["home_town"] = homeTown
	}
	// weeks, months and the time of the day in titles depend on it: an empty one keeps the current one
	if u.Timezone != nil && *u.Timezone != "" {
		if _, err := time.LoadLocation(*u.Timezone); err != nil {
			return nil, errors.New("unknown timezone")
		}
		fields["timezone"] = *u.Timezone
	}
	return fields, nil
}

// UpdateUser changes the account of the user of the request, who confirms it with their password. Only the
// fields sent change. A new password ends the other sessions, the answer carries a new token.
func UpdateUser(c *gin.Context) {
	var update userUpdate
	App := c.MustGet("App").(*app.App)
	db := App.DB
	if err := c.ShouldBindJSON(&update); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	currentUser := middlewares.CurrentUser(c)
	if !utils.CompareHashPassword(update.OldPassword, currentUser.Password) {
		c.JSON(401, gin.H{"error": "wrong password"})
		return
	}

	now := time.Now()
	fields, err := update.fields(now)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if update.Password != "" {
		if update.Password != update.PasswordConfirmation {
			c.JSON(400, gin.H{"error": "passwords don't match"})
			return
		}
		newHash, err := utils.GenerateHashPassword(update.Password)
		if errors.Is(err, bcrypt.ErrPasswordTooLong) {
			c.JSON(400, gin.H{"error": "the password is longer than 72 bytes"})
			return
		}
		if err != nil {
			App.Logger.Error().Err(err).Msg("password not changed: not hashed")
			c.JSON(500, gin.H{"error": "could not generate password hash"})
			return
		}
		fields["password"] = newHash
		// the sessions opened with the old password end, on every device: a stolen token stops working
		fields["sessions_valid_after"] = now
	}

	err = db.UpdateUserFields(currentUser.ID, fields)
	if errors.Is(err, models.ErrEmailTaken) {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		App.Logger.Error().Err(err).Uint("user_id", currentUser.ID).Msg("user not updated")
		c.JSON(500, gin.H{"error": "could not update the user"})
		return
	}

	if update.Password == "" {
		c.JSON(200, gin.H{"message": "user updated successfully"})
		return
	}
	// the token of the request ended with the others: the client goes on with this one
	token, _, err := utils.NewSessionToken(currentUser.ID, currentUser.UUID, currentUser.Role, now)
	if err != nil {
		c.JSON(500, gin.H{"error": "password changed, log in again"})
		return
	}
	c.JSON(200, gin.H{"message": "user updated successfully", "token": token})
}

// currentUserLocation returns the user of the request and their timezone (UTC when unknown):
// weeks and months start at midnight in it
func currentUserLocation(c *gin.Context) (models.Users, *time.Location) {
	user := middlewares.CurrentUser(c)
	return user, activityService.UserLocation(user.Timezone)
}

func Dashboard(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, location := currentUserLocation(c)
	dashboard, err := db.GetDashboard(user.ID, time.Now().In(location))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, dashboard)
}

// Calendar returns the activities of a month, ?month=YYYY-MM
func Calendar(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, location := currentUserLocation(c)
	month, err := time.ParseInLocation("2006-01", c.Query("month"), location)
	if err != nil {
		c.JSON(400, gin.H{"error": "month must be YYYY-MM"})
		return
	}
	calendar, err := db.GetCalendar(user.ID, month)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"month": month.Format("2006-01"), "activities": calendar})
}

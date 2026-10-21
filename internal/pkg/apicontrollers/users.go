package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
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

	var existingUser models.Users

	err := db.Where("Username = ?", user.Username).First(&existingUser).Error

	if existingUser.ID == 0 {
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Error().Err(err).Msg("login refused: database error while looking up the user")
		} else {
			logger.Debug().Msg("login refused: no user with this username (it is case sensitive, the email does not work)")
		}
		c.JSON(400, gin.H{"error": "user does not exist"})
		return
	}
	logger.Debug().Uint("user_id", existingUser.ID).Int("stored_hash_length", len(existingUser.Password)).
		Bool("stored_hash_is_bcrypt", strings.HasPrefix(existingUser.Password, "$2")).Msg("user found")

	if err := utils.CheckPassword(user.Password, existingUser.Password); err != nil {
		logger.Debug().Err(err).Uint("user_id", existingUser.ID).Msg("login refused: password check failed")
		c.JSON(400, gin.H{"error": "invalid password"})
		return
	}
	logger.Debug().Uint("user_id", existingUser.ID).Msg("password ok, issuing token")

	expirationTime := time.Now().Add(720 * time.Hour)

	claims := jwt.MapClaims{
		"authorized": true,
		"role":       existingUser.Role,
		"exp":        expirationTime.Unix(),
		"iss":        "mysportweb",
		"sub":        existingUser.ID,
		"uuid":       existingUser.UUID.String(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(vars.SecretKey))

	if err != nil {
		c.JSON(500, gin.H{"error": "could not generate token"})
		return
	}
	c.SetCookie("mysportweb_session", tokenString, 3600, "/", "localhost", false, true)
	c.JSON(200, gin.H{"token": tokenString})
}

func Register(c *gin.Context) {

	var body types.RegisterBody
	App := c.MustGet("App")
	db := App.(*app.App).DB

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	user := models.Users{Username: body.Username, Password: body.Password, Email: body.Email}

	if user.Username == "" || user.Password == "" || user.Email == "" {
		c.JSON(400, gin.H{"error": "missing required fields"})
		return
	}

	var existingUser models.Users

	db.Where("Username = ?", user.Username).First(&existingUser)

	if existingUser.ID != 0 {
		c.JSON(409, gin.H{"error": "user already exists"})
		return
	}

	var errHash error
	user.Password, errHash = utils.GenerateHashPassword(user.Password)

	if errHash != nil {
		c.JSON(500, gin.H{"error": "could not generate password hash"})
		return
	}

	db.Create(&user)

	if user.ID == 1 {
		user.Role = "Admin"
		db.Save(&user)
	} else {
		user.Role = "User"
		db.Save(&user)
	}

	c.JSON(201, gin.H{"success": "user registered"})
}

func Logout(c *gin.Context) {

	c.JSON(200, gin.H{"success": "user logged out"})
}

func GetUser(c *gin.Context) {
	App := c.MustGet("App")
	db := App.(*app.App).DB
	bearerToken := c.Request.Header.Get("Authorization")
	userUUID, err := utils.GetUserUUID(bearerToken)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	user, err := db.GetUserByUUID(userUUID)

	if err != nil {
		c.JSON(400, gin.H{"error": "user not found"})
		return
	}
	c.JSON(200, user)
}

func UpdateUser(c *gin.Context) {

	var updatedUser models.Users
	var user types.UserBody
	App := c.MustGet("App")
	db := App.(*app.App).DB
	err := c.ShouldBindJSON(&user)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	bearerToken := c.Request.Header.Get("Authorization")
	UserID, err := utils.GetUserID(bearerToken)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	currentUser, err := db.GetUser(UserID)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if !utils.CompareHashPassword(user.OldPassword, currentUser.Password) {
		c.JSON(401, gin.H{
			"error": "wrong password",
		})
		return
	}

	// start from the stored user: id, uuid, username, role and timestamps can't be changed here
	updatedUser = currentUser
	if user.Password != "" {
		if user.Password != user.PasswordConfirmation {
			c.JSON(400, gin.H{"error": "passwords don't match"})
			return
		}
		newHash, err := utils.GenerateHashPassword(user.Password)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		updatedUser.Password = newHash
	}
	updatedUser.Email = user.Email
	updatedUser.DateOfBirth = user.DateOfBirth
	updatedUser.Height = user.Height
	updatedUser.Weight = user.Weight
	updatedUser.MaxHR = user.MaxHR
	updatedUser.Gender = user.Gender
	updatedUser.SecurityDistance = user.SecurityDistance
	updatedUser.HomeTown = user.HomeTown

	err = db.UpdateUser(updatedUser)

	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "user updated successfully"})
}

// currentUserLocation returns the user of the request and their timezone (UTC when unknown):
// weeks and months start at midnight in it. It aborts the request on error.
func currentUserLocation(c *gin.Context) (models.Users, *time.Location, bool) {
	db := c.MustGet("App").(*app.App).DB
	userID, err := utils.GetUserID(c.Request.Header.Get("Authorization"))
	if err != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": err.Error()})
		return models.Users{}, nil, false
	}
	user, err := db.GetUser(userID)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{"error": err.Error()})
		return models.Users{}, nil, false
	}
	location, err := time.LoadLocation(user.Timezone)
	if err != nil || user.Timezone == "" {
		location = time.UTC
	}
	return user, location, true
}

func Dashboard(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	user, location, ok := currentUserLocation(c)
	if !ok {
		return
	}
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
	user, location, ok := currentUserLocation(c)
	if !ok {
		return
	}
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

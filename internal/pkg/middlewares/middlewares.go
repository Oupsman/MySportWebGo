package middlewares

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// userKey is the context key of the user of the request, set by IsAuthorized, IsAdmin and OptionalUser
const userKey = "User"

// authError is a refused authentication, with the HTTP status to answer
type authError struct {
	status int
	err    error
}

func refuse(status int, message string) *authError {
	return &authError{status, errors.New(message)}
}

// authenticate returns the user of the Authorization header, loaded from the database: a token of a
// deleted user is refused. It accepts the session tokens (JWT) of the web application, and the device
// tokens (msw_...) holding all the required permissions. A route without required permissions is
// closed to device tokens: they only get what is explicitly opened to them.
func authenticate(c *gin.Context, required models.Permission) (models.Users, *authError) {
	header := c.GetHeader("Authorization")
	token, err := utils.BearerToken(header)
	if err != nil {
		return models.Users{}, &authError{http.StatusUnauthorized, err}
	}
	App := c.MustGet("App").(*app.App)
	db := App.DB

	if !strings.HasPrefix(token, utils.DeviceTokenPrefix) {
		userUUID, err := utils.GetUserUUID(header)
		if err != nil {
			return models.Users{}, &authError{http.StatusUnauthorized, err}
		}
		user, err := db.GetUserByUUID(userUUID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Users{}, refuse(http.StatusUnauthorized, "unknown user")
		}
		if err != nil {
			return models.Users{}, &authError{http.StatusInternalServerError, err}
		}
		return user, nil
	}

	deviceToken, err := db.GetDeviceTokenByHash(utils.HashDeviceToken(token))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Users{}, refuse(http.StatusUnauthorized, "invalid device token")
	}
	if err != nil {
		return models.Users{}, &authError{http.StatusInternalServerError, err}
	}
	now := time.Now()
	if !deviceToken.ExpiresAt.After(now) {
		return models.Users{}, refuse(http.StatusUnauthorized, "expired device token")
	}
	if deviceToken.User.ID == 0 {
		return models.Users{}, refuse(http.StatusUnauthorized, "unknown user")
	}
	if required == 0 {
		return models.Users{}, refuse(http.StatusForbidden, "device tokens can't use this route")
	}
	if !deviceToken.Permissions.Has(required) {
		return models.Users{}, refuse(http.StatusForbidden,
			"the device token lacks the permissions "+strings.Join((required&^deviceToken.Permissions).Names(), ", "))
	}
	if err := db.TouchDeviceToken(deviceToken.ID, now); err != nil {
		// the request can go on, only the last use date is missed
		App.Logger.Warn().Err(err).Str("device_token", deviceToken.ID.String()).Msg("could not record the use of a device token")
	}
	return deviceToken.User, nil
}

func combine(permissions []models.Permission) models.Permission {
	var all models.Permission
	for _, permission := range permissions {
		all |= permission
	}
	return all
}

func abort(c *gin.Context, authErr *authError) {
	message := authErr.err.Error()
	switch authErr.status {
	case http.StatusUnauthorized:
		message = "unauthorized: " + message
	case http.StatusForbidden:
		message = "forbidden: " + message
	}
	c.AbortWithStatusJSON(authErr.status, gin.H{
		"message": message,
	})
}

// IsAuthorized requires a session token, or a device token holding all the given permissions.
// Without permissions the route is closed to device tokens.
func IsAuthorized(permissions ...models.Permission) gin.HandlerFunc {
	required := combine(permissions)

	return func(c *gin.Context) {
		user, authErr := authenticate(c, required)
		if authErr != nil {
			abort(c, authErr)
			return
		}
		c.Set(userKey, user)
		c.Next()
	}
}

// IsAdmin requires a session token of an admin, device tokens are refused
func IsAdmin() gin.HandlerFunc {

	return func(c *gin.Context) {
		user, authErr := authenticate(c, 0)
		if authErr != nil {
			abort(c, authErr)
			return
		}

		// the role of the database, not the one of the token: a demoted admin loses access immediately
		if user.Role != "Admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "forbidden",
			})
			return
		}

		c.Set(userKey, user)
		c.Next()
	}
}

// OptionalUser sets the user of the request when there is an Authorization header, for routes also open
// to anonymous users. An invalid token is refused rather than treated as anonymous, and a device token
// needs the given permissions as with IsAuthorized.
func OptionalUser(permissions ...models.Permission) gin.HandlerFunc {
	required := combine(permissions)

	return func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.Next()
			return
		}
		user, authErr := authenticate(c, required)
		if authErr != nil {
			abort(c, authErr)
			return
		}
		c.Set(userKey, user)
		c.Next()
	}
}

// CurrentUser returns the user set by IsAuthorized or IsAdmin. It panics on a route without them.
func CurrentUser(c *gin.Context) models.Users {
	return c.MustGet(userKey).(models.Users)
}

// LookupUser returns the user set by OptionalUser, and false for an anonymous request
func LookupUser(c *gin.Context) (models.Users, bool) {
	value, ok := c.Get(userKey)
	if !ok {
		return models.Users{}, false
	}
	user, ok := value.(models.Users)
	return user, ok
}

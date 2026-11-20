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

// userKey is the context key of the user of the request, set by IsAuthorized, IsAdmin, OptionalUser and
// IsDeviceToken; deviceTokenKey the one of the device token, set by IsDeviceToken
const (
	userKey        = "User"
	deviceTokenKey = "DeviceToken"
	sessionKey     = "Session" // the session token of the request, set by identify
)

// authError is a refused authentication, with the HTTP status to answer
type authError struct {
	status int
	err    error
}

func refuse(status int, message string) *authError {
	return &authError{status, errors.New(message)}
}

func isDeviceToken(token string) bool {
	return strings.HasPrefix(token, utils.DeviceTokenPrefix)
}

// credentials returns the token of the request: the one of the Authorization header (device tokens, API
// clients), else the one of the session cookie (the web application); fromCookie tells which
func credentials(c *gin.Context) (token string, fromCookie bool, err error) {
	if header := c.GetHeader("Authorization"); header != "" {
		token, err := utils.BearerToken(header)
		return token, false, err
	}
	if token := sessionCookie(c); token != "" {
		// the cookie only ever holds a session token
		if isDeviceToken(token) {
			return "", true, errors.New("a device token in the session cookie")
		}
		return token, true, nil
	}
	return "", false, errors.New("missing bearer token or session cookie")
}

// identify returns the user of the Authorization header or of the session cookie, loaded from the
// database: a token of a deleted user is refused. It accepts the session tokens (JWT) of the web
// application not logged out nor ended by a password change, and the valid device tokens (msw_...),
// returned for the latter.
func identify(c *gin.Context) (models.Users, *models.DeviceTokens, *authError) {
	token, _, err := credentials(c)
	if err != nil {
		return models.Users{}, nil, &authError{http.StatusUnauthorized, err}
	}
	db := c.MustGet("App").(*app.App).DB

	if !isDeviceToken(token) {
		session, err := utils.ParseSessionToken(token)
		if err != nil {
			return models.Users{}, nil, &authError{http.StatusUnauthorized, err}
		}
		user, err := db.GetUserByUUID(session.UserUUID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Users{}, nil, refuse(http.StatusUnauthorized, "unknown user")
		}
		if err != nil {
			return models.Users{}, nil, &authError{http.StatusInternalServerError, err}
		}
		// the token dates are in seconds: a token issued in the second of the change stays valid, the one
		// returned with the new password
		if session.IssuedAt.Unix() < user.SessionsValidAfter.Unix() {
			return models.Users{}, nil, refuse(http.StatusUnauthorized, "session ended, log in again")
		}
		revoked, err := db.IsSessionRevoked(session.ID)
		if err != nil {
			return models.Users{}, nil, &authError{http.StatusInternalServerError, err}
		}
		if revoked {
			return models.Users{}, nil, refuse(http.StatusUnauthorized, "logged out")
		}
		c.Set(sessionKey, session)
		return user, nil, nil
	}

	deviceToken, err := db.GetDeviceTokenByHash(utils.HashDeviceToken(token))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Users{}, nil, refuse(http.StatusUnauthorized, "invalid device token")
	}
	if err != nil {
		return models.Users{}, nil, &authError{http.StatusInternalServerError, err}
	}
	if !deviceToken.ExpiresAt.After(time.Now()) {
		return models.Users{}, nil, refuse(http.StatusUnauthorized, "expired device token")
	}
	if deviceToken.User.ID == 0 {
		return models.Users{}, nil, refuse(http.StatusUnauthorized, "unknown user")
	}
	return deviceToken.User, &deviceToken, nil
}

// touch records the use of a device token
func touch(c *gin.Context, deviceToken *models.DeviceTokens) {
	App := c.MustGet("App").(*app.App)
	if err := App.DB.TouchDeviceToken(deviceToken.ID, time.Now()); err != nil {
		// the request can go on, only the last use date is missed
		App.Logger.Warn().Err(err).Str("device_token", deviceToken.ID.String()).Msg("could not record the use of a device token")
	}
}

// authenticate returns the user of a session token, or of a device token holding all the required
// permissions. A route without required permissions is closed to device tokens: they only get what is
// explicitly opened to them.
func authenticate(c *gin.Context, required models.Permission) (models.Users, *authError) {
	user, deviceToken, authErr := identify(c)
	if authErr != nil || deviceToken == nil {
		return user, authErr
	}
	if required == 0 {
		return models.Users{}, refuse(http.StatusForbidden, "device tokens can't use this route")
	}
	if !deviceToken.Permissions.Has(required) {
		return models.Users{}, refuse(http.StatusForbidden,
			"the device token lacks the permissions "+strings.Join((required&^deviceToken.Permissions).Names(), ", "))
	}
	touch(c, deviceToken)
	return user, nil
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
	default:
		// the database failed: its error tells the server's internals, it is only logged
		c.MustGet("App").(*app.App).Logger.Error().Err(authErr.err).Str("route", c.FullPath()).
			Msg("authentication failed")
		message = "internal error, see the server logs"
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

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// IsAdmin requires a session token of an admin, device tokens are refused
// func IsAdmin() gin.HandlerFunc {
//
// 	return func(c *gin.Context) {
// 		user, authErr := authenticate(c, 0)
// 		if authErr != nil {
// 			abort(c, authErr)
// 			return
// 		}
//
// 		// the role of the database, not the one of the token: a demoted admin loses access immediately
// 		if user.Role != "Admin" {
// 			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
// 				"message": "forbidden",
// 			})
// 			return
// 		}
//
// 		c.Set(userKey, user)
// 		c.Next()
// 	}
// }

// OptionalUser sets the user of the request when there is an Authorization header or a session cookie, for
// routes also open to anonymous users. An invalid token of the header is refused rather than treated as
// anonymous; the cookie of an ended session is treated as anonymous (the browser keeps it until its
// expiry). A device token needs the given permissions as with IsAuthorized.
func OptionalUser(permissions ...models.Permission) gin.HandlerFunc {
	required := combine(permissions)

	return func(c *gin.Context) {
		_, fromCookie, err := credentials(c)
		if err != nil && !fromCookie {
			// no credentials: anonymous
			c.Next()
			return
		}
		user, authErr := authenticate(c, required)
		if authErr != nil && fromCookie && authErr.status == http.StatusUnauthorized {
			// a cookie of an ended session (logged out elsewhere, password changed): anonymous, the
			// public activities stay visible
			c.Next()
			return
		}
		if authErr != nil {
			abort(c, authErr)
			return
		}
		c.Set(userKey, user)
		c.Next()
	}
}

// IsDeviceToken requires a valid device token, whatever its permissions: for the routes describing the
// token itself. Session tokens are refused.
func IsDeviceToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, deviceToken, authErr := identify(c)
		if authErr == nil && deviceToken == nil {
			authErr = refuse(http.StatusForbidden, "this route is for device tokens")
		}
		if authErr != nil {
			abort(c, authErr)
			return
		}
		touch(c, deviceToken)
		c.Set(userKey, user)
		c.Set(deviceTokenKey, *deviceToken)
		c.Next()
	}
}

// CurrentDeviceToken returns the device token set by IsDeviceToken. It panics on a route without it.
func CurrentDeviceToken(c *gin.Context) models.DeviceTokens {
	return c.MustGet(deviceTokenKey).(models.DeviceTokens)
}

// CurrentSession returns the session token of the request, false for a device token or an anonymous request
func CurrentSession(c *gin.Context) (utils.Session, bool) {
	value, ok := c.Get(sessionKey)
	if !ok {
		return utils.Session{}, false
	}
	session, ok := value.(utils.Session)
	return session, ok
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

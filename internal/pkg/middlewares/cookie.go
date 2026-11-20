package middlewares

import (
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// The session token of the web application is kept by the browser in an HttpOnly cookie: the scripts of the
// page can't read it, a script injected in the page can't steal the session. SameSite=Strict: the browser
// doesn't send it with the requests other sites start; CrossOriginProtection refuses the others.
const (
	// sessionCookieSecure is the name of the cookie over HTTPS: the __Host- prefix makes the browser refuse
	// it unless Secure, for the whole host, set by this host (not by a subdomain)
	sessionCookieSecure = "__Host-msw_session"
	// sessionCookiePlain is its name over HTTP (development, a local network): the browser would drop a
	// Secure cookie
	sessionCookiePlain = "msw_session"
)

// secureCookies tells if the app is served over HTTPS, from DOMAIN
func secureCookies() bool {
	return strings.HasPrefix(vars.Domain, "https://")
}

// SessionCookieName is the name of the session cookie for DOMAIN
func SessionCookieName() string {
	if secureCookies() {
		return sessionCookieSecure
	}
	return sessionCookiePlain
}

// SetSessionCookie gives the browser a session token, until it expires
func SetSessionCookie(c *gin.Context, token string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie makes the browser forget its session cookie
func ClearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteStrictMode,
	})
}

// sessionCookie returns the token of the session cookie of the request, "" without
func sessionCookie(c *gin.Context) string {
	cookie, err := c.Request.Cookie(SessionCookieName())
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SessionToken returns the session token of the request: the one of the Authorization header (not a device
// token), else the one of the cookie; "" without
func SessionToken(c *gin.Context) string {
	if header := c.GetHeader("Authorization"); header != "" {
		token, found := strings.CutPrefix(header, "Bearer ")
		if !found || isDeviceToken(token) {
			return ""
		}
		return token
	}
	return sessionCookie(c)
}

// CrossOriginProtection refuses the requests changing something (not GET, HEAD nor OPTIONS) that a browser
// sends from another origin than the app (DOMAIN): a page of another site, or of another subdomain (same site
// for SameSite), can't use the session cookie (CSRF). The clients that aren't browsers (the desktop
// application, scripts with device tokens) send neither Sec-Fetch-Site nor Origin: let through.
func CrossOriginProtection() gin.HandlerFunc {
	protection := http.NewCrossOriginProtection()
	// the frontend of the development server is on another port than the API: another origin
	if err := protection.AddTrustedOrigin(vars.Domain); err != nil {
		panic("DOMAIN must be an origin, scheme://host[:port]: " + err.Error())
	}
	return func(c *gin.Context) {
		if err := protection.Check(c.Request); err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden: cross-origin request"})
			return
		}
		c.Next()
	}
}

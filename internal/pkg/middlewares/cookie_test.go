package middlewares

import (
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// the known-device cookie is for its username, until it expires, and can't be forged
func TestKnownDeviceCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedKey, savedDomain := vars.SecretKey, vars.Domain
	t.Cleanup(func() { vars.SecretKey, vars.Domain = savedKey, savedDomain })
	vars.SecretKey, vars.Domain = "known-device-test-secret", "https://sport.example.com"
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
	SetKnownDeviceCookie(c, "Bob", now)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-msw_known" || !cookies[0].HttpOnly || !cookies[0].Secure ||
		cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie %+v", cookies)
	}
	known := func(value, username string, at time.Time) bool {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/login", nil)
		if value != "" {
			c.Request.AddCookie(&http.Cookie{Name: "__Host-msw_known", Value: value})
		}
		return IsKnownDevice(c, username, at)
	}
	value := cookies[0].Value
	if !known(value, "bob", now.Add(time.Hour)) {
		t.Error("its username, the case ignored: unknown")
	}
	if known(value, "alice", now) {
		t.Error("known for another username")
	}
	if known(value, "bob", now.Add(KnownDeviceLifetime)) {
		t.Error("known once expired")
	}
	if known("", "bob", now) {
		t.Error("known without cookie")
	}
	// another expiry, the signature kept
	parts := strings.Split(value, ".")
	if known(parts[0]+".9999999999."+parts[2], "bob", now) {
		t.Error("expiry changed: still known")
	}
	// signed with another secret
	vars.SecretKey = "another secret"
	if known(value, "bob", now) {
		t.Error("known after the secret changed")
	}
}

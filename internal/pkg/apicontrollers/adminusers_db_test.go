package apicontrollers

import (
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// adminRequest sends a request with the session cookie, and a code of the authenticator when not empty
func adminRequest(client sessionClient, method, path string, cookie *http.Cookie, totpCode, body string) (int, map[string]interface{}) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	if totpCode != "" {
		req.Header.Set(TOTPHeader, totpCode)
	}
	w := httptest.NewRecorder()
	client.router.ServeHTTP(w, req)
	answer := map[string]interface{}{}
	json.Unmarshal(w.Body.Bytes(), &answer)
	return w.Code, answer
}

func TestAdminUsers(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	client := twoFactorRouter(t, db)
	r := client.router
	r.GET("/admin/users", middlewares.IsAdmin(), GetUsers)
	r.POST("/admin/users/:id/role", middlewares.IsAdmin(), SetUserRole)
	r.POST("/admin/users/:id/disable", middlewares.IsAdmin(), DisableUser)
	r.POST("/admin/users/:id/enable", middlewares.IsAdmin(), EnableUser)
	r.POST("/admin/users/:id/password-reset", middlewares.IsAdmin(), CreatePasswordReset)
	r.DELETE("/admin/users/:id", middlewares.IsAdmin(), AdminDeleteUser)
	r.POST("/password-reset", ResetPassword)

	users := createUsers(t, db, "password", "boss", "alice", "bob")
	boss, alice, bob := users[0], users[1], users[2]
	db.Model(&models.Users{}).Where("id = ?", boss.ID).Update("role", models.RoleAdmin)
	secret := enableTOTP(t, db, "boss")
	admin := client.loginCookieMFA(db, "boss", "password", secret)
	code := func() string {
		// a minute later each time: every action uses a new code
		db.Model(&models.Users{}).Where("id = ?", boss.ID).Update("totp_last_step", 0)
		return nextCode(t, db, "boss", secret)
	}
	path := func(u models.Users, action string) string { return fmt.Sprintf("/admin/users/%d%s", u.ID, action) }

	// a user isn't an admin
	if status, _ := adminRequest(client, http.MethodGet, "/admin/users", client.loginCookie("alice", "password"), "", ""); status != http.StatusForbidden {
		t.Errorf("user listing: %d", status)
	}
	status, answer := adminRequest(client, http.MethodGet, "/admin/users", admin, "", "")
	list, _ := answer["users"].([]interface{})
	if status != http.StatusOK || len(list) != 3 {
		t.Fatalf("list: %d %v", status, answer)
	}
	first, _ := list[0].(map[string]interface{})
	if first["username"] != "alice" || first["two_factor"] != false || first["disk_bytes"] == nil {
		t.Errorf("first user: %v", first)
	}

	// the changes need a code, a good one, used once
	if status, answer := adminRequest(client, http.MethodPost, path(alice, "/role"), admin, "", `{"role": "Admin"}`); status != http.StatusForbidden || answer["code"] != "totp_required" {
		t.Errorf("without code: %d %v", status, answer)
	}
	if status, answer := adminRequest(client, http.MethodPost, path(alice, "/role"), admin, "000000", `{"role": "Admin"}`); status != http.StatusForbidden || answer["code"] != "totp_invalid" {
		t.Errorf("wrong code: %d %v", status, answer)
	}
	good := code()
	if status, answer := adminRequest(client, http.MethodPost, path(alice, "/role"), admin, good, `{"role": "Admin"}`); status != http.StatusOK {
		t.Errorf("promote: %d %v", status, answer)
	}
	if status, _ := adminRequest(client, http.MethodPost, path(alice, "/role"), admin, good, `{"role": "User"}`); status != http.StatusForbidden {
		t.Errorf("a code used twice: %d", status)
	}
	if status, _ := adminRequest(client, http.MethodPost, path(alice, "/role"), admin, code(), `{"role": "User"}`); status != http.StatusOK {
		t.Errorf("demote: %d", status)
	}
	// an invalid request doesn't use up the code: checked before it
	good = code()
	if status, _ := adminRequest(client, http.MethodPost, path(alice, "/role"), admin, good, `{"role": "King"}`); status != http.StatusBadRequest {
		t.Errorf("unknown role: %d", status)
	}
	if status, _ := adminRequest(client, http.MethodPost, path(boss, "/disable"), admin, good, ""); status != http.StatusBadRequest {
		t.Errorf("disabling oneself: %d", status)
	}

	// deleting: disabled first
	if status, _ := adminRequest(client, http.MethodDelete, path(bob, ""), admin, good, ""); status != http.StatusConflict {
		t.Errorf("deleting an enabled account: %d", status)
	}
	if status, _ := adminRequest(client, http.MethodPost, path(bob, "/disable"), admin, good, ""); status != http.StatusOK {
		t.Fatalf("disable: %d", status)
	}
	if status, answer, _ := client.browser(http.MethodPost, "/login", nil, `{"username": "bob", "password": "password"}`); status != http.StatusForbidden {
		t.Errorf("login of the disabled bob: %d %v", status, answer)
	}
	if status, _ := adminRequest(client, http.MethodDelete, path(bob, ""), admin, "", ""); status != http.StatusForbidden {
		t.Errorf("deleting without code: %d", status)
	}
	if status, _ := adminRequest(client, http.MethodDelete, path(bob, ""), admin, code(), ""); status != http.StatusOK {
		t.Fatalf("delete: %d", status)
	}
	var left int64
	db.Unscoped().Model(&models.Users{}).Where("id = ?", bob.ID).Count(&left)
	if left != 0 {
		t.Error("bob is still there")
	}
	if status, _ := adminRequest(client, http.MethodPost, path(bob, "/enable"), admin, code(), ""); status != http.StatusNotFound {
		t.Errorf("enabling a deleted user: %d", status)
	}

	// a password reset link: once, it ends the sessions
	aliceCookie := client.loginCookie("alice", "password")
	status, answer = adminRequest(client, http.MethodPost, path(alice, "/password-reset"), admin, code(), "")
	link, _ := answer["link"].(string)
	if status != http.StatusCreated || !strings.HasPrefix(link, "https://sport.example.com/#/reset-password?code=") {
		t.Fatalf("reset link: %d %v", status, answer)
	}
	parsed, _ := url.Parse(strings.Replace(link, "/#/", "/", 1))
	resetCode := parsed.Query().Get("code")
	reset := func(code, password string) int {
		status, _, _ := client.browser(http.MethodPost, "/password-reset", nil,
			`{"code": "`+code+`", "password": "`+password+`"}`)
		return status
	}
	if status := reset(resetCode, "short"); status != http.StatusBadRequest {
		t.Errorf("short password: %d", status)
	}
	if status := reset("nope", "new password"); status != http.StatusBadRequest {
		t.Errorf("unknown code: %d", status)
	}
	if status := reset(resetCode, "new password"); status != http.StatusOK {
		t.Fatalf("reset: %d", status)
	}
	if status := reset(resetCode, "another one"); status != http.StatusBadRequest {
		t.Errorf("a link used twice: %d", status)
	}
	if status, _ := rawRequest(client, http.MethodGet, "/session", aliceCookie, ""); status != http.StatusUnauthorized {
		t.Errorf("a session before the reset: %d", status)
	}
	client.loginCookie("alice", "new password")
}

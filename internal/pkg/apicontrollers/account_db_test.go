package apicontrollers

import (
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOwnAccount(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	t.Chdir(t.TempDir())
	client := twoFactorRouter(t, db)
	client.router.GET("/export", middlewares.IsAuthorized(), ExportAccount)
	client.router.POST("/delete", middlewares.IsAuthorized(), DeleteAccount)
	users := createUsers(t, db, "password", "alice", "boss")
	db.Model(&models.Users{}).Where("id = ?", users[1].ID).Update("role", models.RoleAdmin)
	alice := client.loginCookie("alice", "password")

	// the export: a zip
	req := httptest.NewRequest(http.MethodGet, "/export", nil)
	req.AddCookie(&http.Cookie{Name: alice.Name, Value: alice.Value})
	w := httptest.NewRecorder()
	client.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if _, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len())); err != nil {
		t.Errorf("not a zip: %v", err)
	}

	// deleting: the username typed, the password, then the code with the two-factor authentication
	for _, refused := range []struct {
		body string
		code int
	}{
		{`{"password": "password", "username": "Alice"}`, http.StatusBadRequest},
		{`{"password": "nope", "username": "alice"}`, http.StatusUnauthorized},
	} {
		if code, _ := rawRequest(client, http.MethodPost, "/delete", alice, refused.body); code != refused.code {
			t.Errorf("%s: %d, want %d", refused.body, code, refused.code)
		}
	}
	secret := enableTOTP(t, db, "alice")
	alice = client.loginCookieMFA(db, "alice", "password", secret)
	db.Model(&models.Users{}).Where("username = ?", "alice").Update("totp_last_step", 0)
	if code, _ := rawRequest(client, http.MethodPost, "/delete", alice, `{"password": "password", "username": "alice"}`); code != http.StatusBadRequest {
		t.Errorf("without code: %d", code)
	}
	code, _ := rawRequest(client, http.MethodPost, "/delete", alice,
		`{"password": "password", "username": "alice", "code": "`+nextCode(t, db, "alice", secret)+`"}`)
	if code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	var left int64
	db.Unscoped().Model(&models.Users{}).Where("username = ?", "alice").Count(&left)
	if left != 0 {
		t.Error("alice is still there")
	}

	// the last admin can't
	db.Model(&models.Users{}).Where("username = ?", "boss").Update("totp_enabled_at", nil)
	boss := client.loginCookie("boss", "password")
	if code, body := rawRequest(client, http.MethodPost, "/delete", boss, `{"password": "password", "username": "boss"}`); code != http.StatusConflict {
		t.Errorf("last admin: %d %s", code, body)
	}
}

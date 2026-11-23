package apicontrollers

import (
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/staticmap"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/services/activityService"
	"MySportWeb/services/activityService/convert/converttest"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// an upload above the quota of the user is refused; an admin gives a user their own quota, none, or the one
// of the instance again
func TestStorageQuota(t *testing.T) {
	db := testDB(t)
	t.Chdir(t.TempDir())
	savedTiles, savedQuota := activityService.MapTiles, vars.UserQuota
	activityService.MapTiles = staticmap.NewTiles(t.TempDir(), "") // offline: blank tiles
	t.Cleanup(func() { activityService.MapTiles, vars.UserQuota = savedTiles, savedQuota })
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	// the instance gives 1 KB: less than any activity
	vars.UserQuota = 1 << 10

	client := twoFactorRouter(t, db)
	r := client.router
	r.POST("/activity", middlewares.IsAuthorized(models.ActivitiesCreate), UploadActivity)
	r.GET("/user/storage", middlewares.IsAuthorized(), GetStorage)
	r.GET("/admin/users", middlewares.IsAdmin(), GetUsers)
	r.POST("/admin/users/:id/quota", middlewares.IsAdmin(), SetUserQuota)

	users := createUsers(t, db, "password", "boss", "alice")
	boss, alice := users[0], users[1]
	db.Model(&models.Users{}).Where("id = ?", boss.ID).Update("role", models.RoleAdmin)
	secret := enableTOTP(t, db, "boss")
	admin := client.loginCookieMFA(db, "boss", "password", secret)
	aliceCookie := client.loginCookie("alice", "password")
	code := func() string {
		db.Model(&models.Users{}).Where("id = ?", boss.ID).Update("totp_last_step", 0)
		return nextCode(t, db, "boss", secret)
	}
	setQuota := func(user models.Users, body string) (int, map[string]interface{}) {
		t.Helper()
		return adminRequest(client, http.MethodPost, fmt.Sprintf("/admin/users/%d/quota", user.ID), admin, code(), body)
	}
	upload := func(name string) (int, map[string]interface{}) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", name)
		part.Write(converttest.GPX(false))
		form.WriteField("sport", "cycling")
		form.Close()
		req := httptest.NewRequest(http.MethodPost, "/activity", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: aliceCookie.Name, Value: aliceCookie.Value})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		answer := map[string]interface{}{}
		json.Unmarshal(w.Body.Bytes(), &answer)
		return w.Code, answer
	}
	storage := func() map[string]interface{} {
		t.Helper()
		status, answer := adminRequest(client, http.MethodGet, "/user/storage", aliceCookie, "", "")
		if status != http.StatusOK {
			t.Fatalf("storage: %d %v", status, answer)
		}
		return answer
	}

	// above the quota of the instance: refused, nothing stored
	if status, answer := upload("ride.gpx"); status != http.StatusInsufficientStorage || answer["code"] != QuotaExceeded {
		t.Fatalf("upload above the quota: %d %v", status, answer)
	}
	if s := storage(); s["used"] != float64(0) || s["quota"] != float64(1<<10) {
		t.Errorf("storage after the refusal: %v", s)
	}

	// the admin gives alice 10 MB
	if status, answer := setQuota(alice, `{"quota_bytes": 10485760}`); status != http.StatusOK || answer["quota"] != float64(10<<20) {
		t.Fatalf("10 MB quota: %d %v", status, answer)
	}
	if status, answer := upload("ride.gpx"); status != http.StatusOK {
		t.Fatalf("upload within 10 MB: %d %v", status, answer)
	}
	if s := storage(); s["used"].(float64) <= 0 || s["quota"] != float64(10<<20) {
		t.Errorf("storage after the upload: %v", s)
	}
	// the admins see it
	_, answer := adminRequest(client, http.MethodGet, "/admin/users", admin, "", "")
	for _, u := range answer["users"].([]interface{}) {
		user := u.(map[string]interface{})
		switch user["username"] {
		case "alice":
			if user["quota_bytes"] != float64(10<<20) || user["quota"] != float64(10<<20) {
				t.Errorf("alice listed %v", user)
			}
		case "boss":
			if user["quota_bytes"] != nil || user["quota"] != float64(1<<10) {
				t.Errorf("boss listed %v", user)
			}
		}
	}

	// no quota
	if status, answer := setQuota(alice, `{"quota_bytes": 0}`); status != http.StatusOK || answer["quota"] != float64(0) {
		t.Fatalf("no quota: %d %v", status, answer)
	}
	if s := storage(); s["quota"] != float64(0) {
		t.Errorf("storage without quota: %v", s)
	}
	// the one of the instance again: alice is above it
	if status, answer := setQuota(alice, `{"quota_bytes": null}`); status != http.StatusOK || answer["quota"] != float64(1<<10) {
		t.Fatalf("instance quota: %d %v", status, answer)
	}
	if status, _ := upload("another.gpx"); status != http.StatusInsufficientStorage {
		t.Errorf("upload above the instance quota again: %d", status)
	}
	// an admin changes their own quota too
	if status, answer := setQuota(boss, `{"quota_bytes": 5368709120}`); status != http.StatusOK {
		t.Errorf("own quota: %d %v", status, answer)
	}

	// refused: a missing or negative quota, without code, by a user
	for name, body := range map[string]string{"missing": `{}`, "negative": `{"quota_bytes": -1}`, "text": `{"quota_bytes": "1g"}`} {
		if status, answer := setQuota(alice, body); status != http.StatusBadRequest {
			t.Errorf("%s quota: %d %v", name, status, answer)
		}
	}
	path := fmt.Sprintf("/admin/users/%d/quota", alice.ID)
	if status, _ := adminRequest(client, http.MethodPost, path, admin, "", `{"quota_bytes": 0}`); status != http.StatusForbidden {
		t.Errorf("without code: %d", status)
	}
	if status, _ := adminRequest(client, http.MethodPost, path, aliceCookie, "", `{"quota_bytes": 0}`); status != http.StatusForbidden {
		t.Errorf("by a user: %d", status)
	}
	// the refused changes left the instance quota
	var stored models.Users
	db.First(&stored, alice.ID)
	if stored.QuotaBytes != nil {
		t.Errorf("stored quota %v, want none of her own", *stored.QuotaBytes)
	}
}

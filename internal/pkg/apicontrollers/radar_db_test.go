package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func TestSetRadarEventsShown(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User"}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	db.First(&alice, alice.ID)
	if !alice.ShowRadarEvents {
		t.Fatal("the radar events are shown by default")
	}
	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.PUT("/shown", middlewares.IsAuthorized(), SetRadarEventsShown)
	call := func(body string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/shown", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	shown := func() bool {
		t.Helper()
		var u models.Users
		if err := db.First(&u, alice.ID).Error; err != nil {
			t.Fatal(err)
		}
		return u.ShowRadarEvents
	}

	// no password: a switch of the map
	if status := call(`{"shown":false}`); status != http.StatusOK || shown() {
		t.Fatalf("hide: %d, shown %v", status, shown())
	}
	for _, bad := range []string{`{}`, `{"shown":"no"}`, `not json`} {
		if status := call(bad); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, status)
		}
	}
	if shown() {
		t.Error("a refused request changed the setting")
	}
	if status := call(`{"shown":true}`); status != http.StatusOK || !shown() {
		t.Fatalf("show: %d, shown %v", status, shown())
	}
}

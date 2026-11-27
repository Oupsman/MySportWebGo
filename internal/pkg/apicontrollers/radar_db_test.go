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
	if !alice.ShowRadarApproaching || !alice.ShowRadarFast {
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
	call := func(body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/shown", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	shown := func() [2]bool {
		t.Helper()
		var u models.Users
		if err := db.First(&u, alice.ID).Error; err != nil {
			t.Fatal(err)
		}
		return [2]bool{u.ShowRadarApproaching, u.ShowRadarFast}
	}

	// no password: switches of the map, each level alone; the answer tells both
	if status, body := call(`{"approaching":false}`); status != http.StatusOK || shown() != [2]bool{false, true} ||
		body != `{"approaching":false,"fast":true}` {
		t.Fatalf("hide the approaching ones: %d %s, shown %v", status, body, shown())
	}
	if status, _ := call(`{"fast":false}`); status != http.StatusOK || shown() != [2]bool{false, false} {
		t.Fatalf("hide the fast ones: %d, shown %v", status, shown())
	}
	for _, bad := range []string{`{}`, `{"shown":true}`, `{"fast":"no"}`, `not json`} {
		if status, _ := call(bad); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, status)
		}
	}
	if shown() != [2]bool{false, false} {
		t.Error("a refused request changed the settings")
	}
	if status, _ := call(`{"approaching":true,"fast":true}`); status != http.StatusOK || shown() != [2]bool{true, true} {
		t.Fatalf("show both: %d, shown %v", status, shown())
	}
}

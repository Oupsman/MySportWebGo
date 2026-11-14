package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

func testDB(t *testing.T) *models.DB {
	dsn := os.Getenv("MYSPORTWEB_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSPORTWEB_TEST_DSN is not set")
	}
	db, err := models.ConnectToDB("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "test") {
		t.Fatalf("refusing to empty database %q, its name must contain \"test\"", name)
	}
	if err := models.CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("TRUNCATE revoked_sessions, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

type sessionClient struct {
	t      *testing.T
	router *gin.Engine
}

func (s sessionClient) send(method, path, token, body string) (int, map[string]string) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	answer := map[string]string{}
	var raw map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &raw)
	for k, v := range raw {
		if text, ok := v.(string); ok {
			answer[k] = text
		}
	}
	return w.Code, answer
}

func (s sessionClient) login(username, password string) (int, map[string]string) {
	return s.send(http.MethodPost, "/login", "", `{"username": "`+username+`", "password": "`+password+`"}`)
}

func TestSessions(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	// a cheap hash: the test logs in many times
	hash, err := bcrypt.GenerateFromPassword([]byte("old password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		user := models.Users{Username: name, Email: name + "@test.local", Password: string(hash), Role: "User"}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/login", Login)
	router.GET("/logout", middlewares.IsAuthorized(), Logout)
	router.GET("/user", middlewares.IsAuthorized(), GetUser)
	router.POST("/user", middlewares.IsAuthorized(), UpdateUser)
	client := sessionClient{t, router}

	code, answer := client.login("alice", "old password")
	if code != http.StatusOK || answer["token"] == "" {
		t.Fatalf("login: %d %v", code, answer)
	}
	phone := answer["token"]
	_, answer = client.login("alice", "old password")
	laptop := answer["token"]

	// a wrong password and an unknown user get the same answer
	_, wrongPassword := client.login("bob", "nope")
	_, unknownUser := client.login("nobody", "nope")
	if wrongPassword["error"] != unknownUser["error"] || wrongPassword["error"] == "" {
		t.Errorf("wrong password %q, unknown user %q", wrongPassword["error"], unknownUser["error"])
	}

	// logging out ends that session only
	if code, answer := client.send(http.MethodGet, "/logout", phone, ""); code != http.StatusOK {
		t.Fatalf("logout: %d %v", code, answer)
	}
	if code, _ := client.send(http.MethodGet, "/user", phone, ""); code != http.StatusUnauthorized {
		t.Errorf("logged out token: %d", code)
	}
	if code, _ := client.send(http.MethodGet, "/user", laptop, ""); code != http.StatusOK {
		t.Errorf("other session after a logout: %d", code)
	}

	// changing the password ends the sessions, the answer carries a new one
	code, answer = client.send(http.MethodPost, "/user", laptop, `{"oldpassword": "old password", `+
		`"newPassword": "new password", "newPasswordConfirmation": "new password", "email": "alice@test.local"}`)
	if code != http.StatusOK || answer["token"] == "" {
		t.Fatalf("password change: %d %v", code, answer)
	}
	renewed := answer["token"]
	if code, _ := client.send(http.MethodGet, "/user", laptop, ""); code != http.StatusUnauthorized {
		t.Errorf("session opened with the old password: %d", code)
	}
	if code, _ := client.send(http.MethodGet, "/user", renewed, ""); code != http.StatusOK {
		t.Errorf("token returned with the new password: %d", code)
	}
	if code, _ := client.login("alice", "new password"); code != http.StatusOK {
		t.Errorf("login with the new password: %d", code)
	}
	// the profile change keeps the sessions
	code, _ = client.send(http.MethodPost, "/user", renewed, `{"oldpassword": "new password", "email": "alice@test.local", "height": 180}`)
	if code != http.StatusOK {
		t.Errorf("profile change: %d", code)
	}
	if code, _ := client.send(http.MethodGet, "/user", renewed, ""); code != http.StatusOK {
		t.Errorf("session after a profile change: %d", code)
	}

	// bob had one failure above: 4 more lock his username, even with the right password, other users go on
	for i := 0; i < maxLoginFailures-1; i++ {
		if code, _ := client.login("Bob", "nope"); code != http.StatusBadRequest {
			t.Fatalf("failure %d: %d", i+2, code)
		}
	}
	if code, _ := client.login("bob", "old password"); code != http.StatusTooManyRequests {
		t.Errorf("locked username: %d", code)
	}
	if code, _ := client.login("alice", "new password"); code != http.StatusOK {
		t.Errorf("another user while bob is locked: %d", code)
	}
}

package middlewares

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
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
	if err := db.Exec("TRUNCATE device_tokens, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAuthorization(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	user := models.Users{Username: "auth", Email: "auth@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&user, user.ID).Error; err != nil { // the uuid is set by the database
		t.Fatal(err)
	}

	session, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(), "iss": "mysportweb", "sub": user.ID, "uuid": user.UUID.String(),
	}).SignedString([]byte(vars.SecretKey))
	if err != nil {
		t.Fatal(err)
	}
	device := func(permissions models.Permission, expiresAt time.Time) string {
		token, hash := utils.NewDeviceToken()
		if err := db.CreateDeviceToken(&models.DeviceTokens{UserID: user.ID, Name: "test", Hash: hash,
			Permissions: permissions, ExpiresAt: expiresAt}); err != nil {
			t.Fatal(err)
		}
		return token
	}
	later := time.Now().Add(time.Hour)
	reader := device(models.ActivitiesRead, later)
	expired := device(models.ActivitiesRead, time.Now().Add(-time.Minute))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	ok := func(c *gin.Context) {
		if CurrentUser(c).ID != user.ID {
			t.Error("wrong user in the context")
		}
		c.Status(http.StatusOK)
	}
	router.GET("/read", IsAuthorized(models.ActivitiesRead), ok)
	router.GET("/delete", IsAuthorized(models.ActivitiesDelete), ok)
	router.GET("/session", IsAuthorized(), ok)
	router.GET("/public", OptionalUser(models.ActivitiesRead), func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, test := range []struct {
		name, path, token string
		want              int
	}{
		{"session on a permission route", "/read", session, 200},
		{"session on a session route", "/session", session, 200},
		{"device with the permission", "/read", reader, 200},
		{"device without the permission", "/delete", reader, 403},
		{"device on a session route", "/session", reader, 403},
		{"expired device", "/read", expired, 401},
		{"unknown device", "/read", utils.DeviceTokenPrefix + "nope", 401},
		{"no token", "/read", "", 401},
		{"anonymous on an optional route", "/public", "", 200},
		{"device on an optional route", "/public", reader, 200},
		{"invalid token on an optional route", "/public", "garbage", 401},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.want {
			t.Errorf("%s: status %d, want %d (%s)", test.name, recorder.Code, test.want, recorder.Body.String())
		}
	}

	var used models.DeviceTokens
	if err := db.Where("hash = ?", utils.HashDeviceToken(reader)).First(&used).Error; err != nil {
		t.Fatal(err)
	}
	if used.LastUsedAt == nil {
		t.Error("the use of the device token was not recorded")
	}

	// a deleted user's tokens stop working
	if err := db.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{session, reader} {
		request := httptest.NewRequest(http.MethodGet, "/read", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != 401 {
			t.Errorf("deleted user: status %d, want 401", recorder.Code)
		}
	}
}

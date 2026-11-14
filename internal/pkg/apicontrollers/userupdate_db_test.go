package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

func TestUpdateUser(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	birth := time.Date(1980, 5, 17, 0, 0, 0, 0, time.UTC)
	alice := models.Users{Username: "alice", Email: "alice@test.local", Password: string(hash), Role: "User",
		DateOfBirth: birth, Weight: 72, Height: 178, MaxHR: 185, Gender: 1, Unit: 1, SecurityDistance: 800,
		HomeTown: "Grenoble", Timezone: "Europe/Paris"}
	bob := models.Users{Username: "bob", Email: "bob@test.local", Password: string(hash), Role: "User"}
	for _, u := range []*models.Users{&alice, &bob} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&alice, alice.ID).Error; err != nil { // the uuid is set by the database
		t.Fatal(err)
	}
	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/user", middlewares.IsAuthorized(), UpdateUser)
	client := sessionClient{t: t, router: router}
	update := func(body string) (int, map[string]string) {
		return client.send(http.MethodPost, "/user", token, body)
	}
	stored := func() models.Users {
		var u models.Users
		if err := db.First(&u, alice.ID).Error; err != nil {
			t.Fatal(err)
		}
		return u
	}

	// only the weight sent: the rest is kept (the email, date of birth... were erased)
	if code, answer := update(`{"oldpassword": "password", "weight": 70}`); code != http.StatusOK {
		t.Fatalf("weight only: %d %v", code, answer)
	}
	got := stored()
	if got.Weight != 70 || got.Email != alice.Email || !got.DateOfBirth.Equal(birth) || got.Height != 178 ||
		got.MaxHR != 185 || got.Gender != 1 || got.Unit != 1 || got.SecurityDistance != 800 ||
		got.HomeTown != "Grenoble" || got.Timezone != "Europe/Paris" || got.Password != string(hash) {
		t.Errorf("after changing the weight only: %+v", got)
	}

	// what another request changed meanwhile is kept: the target weight has its own route
	if err := db.SetWeightObjective(alice.ID, 65); err != nil {
		t.Fatal(err)
	}
	// the unit was never saved; 0 and an empty home town are values, not omissions
	code, answer := update(`{"oldpassword": "password", "unit": 0, "security_distance": 0, "home_town": " Lyon ",
		"email": " Alice@Example.org ", "max_hr": 0}`)
	if code != http.StatusOK {
		t.Fatalf("several fields: %d %v", code, answer)
	}
	got = stored()
	if got.Unit != 0 || got.SecurityDistance != 0 || got.HomeTown != "Lyon" || got.Email != "Alice@Example.org" ||
		got.MaxHR != 0 || got.WeightObjective != 65 || got.Weight != 70 {
		t.Errorf("after several fields: %+v", got)
	}

	for _, tt := range []struct {
		name, body string
		want       int
	}{
		{"wrong password", `{"oldpassword": "nope", "weight": 80}`, http.StatusUnauthorized},
		{"bob's email", `{"oldpassword": "password", "email": "BOB@test.local"}`, http.StatusConflict},
		{"empty email", `{"oldpassword": "password", "email": ""}`, http.StatusBadRequest},
		{"invalid email", `{"oldpassword": "password", "email": "alice"}`, http.StatusBadRequest},
		{"born tomorrow", `{"oldpassword": "password", "date_of_birth": "` +
			time.Now().Add(48*time.Hour).Format(time.RFC3339) + `"}`, http.StatusBadRequest},
		{"weight", `{"oldpassword": "password", "weight": 900}`, http.StatusBadRequest},
		{"height", `{"oldpassword": "password", "height": 400}`, http.StatusBadRequest},
		{"max heart rate", `{"oldpassword": "password", "max_hr": 300}`, http.StatusBadRequest},
		{"gender", `{"oldpassword": "password", "gender": 2}`, http.StatusBadRequest},
		{"unit", `{"oldpassword": "password", "unit": -1}`, http.StatusBadRequest},
		{"timezone", `{"oldpassword": "password", "timezone": "Mars/Olympus"}`, http.StatusBadRequest},
		// a refused update changes nothing, even its valid fields
		{"valid and invalid fields", `{"oldpassword": "password", "weight": 90, "gender": 7}`, http.StatusBadRequest},
	} {
		if code, answer := update(tt.body); code != tt.want {
			t.Errorf("%s: %d %v, want %d", tt.name, code, answer, tt.want)
		}
	}
	if after := stored(); after.Weight != 70 || after.Email != "Alice@Example.org" || after.Gender != 1 {
		t.Errorf("changed by refused updates: %+v", after)
	}

	// her own email in another case is hers
	if code, answer := update(`{"oldpassword": "password", "email": "alice@example.org"}`); code != http.StatusOK {
		t.Errorf("own email, other case: %d %v", code, answer)
	}
	// nothing sent: nothing changes, no error
	if code, answer := update(`{"oldpassword": "password"}`); code != http.StatusOK {
		t.Errorf("empty update: %d %v", code, answer)
	}
}

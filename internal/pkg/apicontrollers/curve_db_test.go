package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestPowerCurveAPI(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User", Timezone: "Europe/Paris", Weight: 70}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&alice, alice.ID).Error; err != nil {
		t.Fatal(err)
	}
	// a ride at 00:30 in Paris on May 2 (22:30 UTC on May 1)
	ride := models.Activity{ID: uuid.New(), UserID: alice.ID, Title: "night ride", Sport: "cycling",
		Date:       time.Date(2026, 5, 1, 22, 30, 0, 0, time.UTC),
		PowerCurve: types.PowerCurve{{Duration: 1, Watts: 900}, {Duration: 1200, Watts: 280}}}
	if err := db.Omit("User", "Equipment").Create(&ride).Error; err != nil {
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
	router.GET("/user/powercurve", middlewares.IsAuthorized(models.ActivitiesRead), PowerCurve)
	client := jsonClient{router: router, token: token}

	points := func(query string) int {
		t.Helper()
		code, answer := client.send(http.MethodGet, "/user/powercurve"+query, "")
		if code != http.StatusOK {
			t.Fatalf("%s: %d %v", query, code, answer)
		}
		list, _ := answer["points"].([]interface{})
		return len(list)
	}
	// the days are the user's: the ride is on May 2 in Paris
	if n := points("?from=2026-05-02&to=2026-05-02"); n != 2 {
		t.Errorf("May 2: %d points", n)
	}
	if n := points("?to=2026-05-01"); n != 0 {
		t.Errorf("until May 1: %d points", n)
	}
	code, answer := client.send(http.MethodGet, "/user/powercurve", "")
	list, _ := answer["points"].([]interface{})
	first, _ := list[0].(map[string]interface{})
	if code != http.StatusOK || first["watts"] != 900.0 || first["watts_per_kg"] != 12.86 ||
		first["activity_id"] != ride.ID.String() || answer["durations"] == nil {
		t.Errorf("all: %d %v", code, answer)
	}
	for _, bad := range []string{"?from=yesterday", "?from=2026-05-03&to=2026-05-02"} {
		if code, _ := client.send(http.MethodGet, "/user/powercurve"+bad, ""); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
	}
}

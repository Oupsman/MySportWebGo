package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/health"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func TestHrZonesAndReadiness(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	if err := db.Exec("TRUNCATE activities, health_data CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := models.Users{Username: "zones", Email: "zones@test.local", Role: "User", Timezone: "Europe/Paris"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&user, user.ID).Error; err != nil { // the uuid is set by the database
		t.Fatal(err)
	}
	now := time.Now()
	trimp := 80.0
	activity := models.Activity{UserID: user.ID, Date: now, Trimp: &trimp, HrZones: types.FloatArray{60, 120, 0, 0, 30}}
	if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateHealthData(&models.HealthData{UserID: user.ID, Date: now, RestHR: 52}); err != nil {
		t.Fatal(err)
	}
	token, _, err := utils.NewSessionToken(user.ID, user.UUID, user.Role, now)
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/user/hrzones", middlewares.IsAuthorized(models.ActivitiesRead), HrZones)
	router.GET("/user/readiness", middlewares.IsAuthorized(models.ActivitiesRead, models.HealthDatasRead), GetReadiness)
	get := func(path string, answer interface{}) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if answer != nil && w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), answer); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code
	}

	var zones struct {
		Limits []float64             `json:"limits"`
		Weeks  []types.WeeklyHrZones `json:"weeks"`
	}
	if code := get("/user/hrzones?weeks=3", &zones); code != http.StatusOK {
		t.Fatalf("zones: %d", code)
	}
	if len(zones.Weeks) != 3 || len(zones.Limits) != 4 ||
		zones.Weeks[2].Zones[0] != 60 || zones.Weeks[2].Zones[1] != 120 || zones.Weeks[2].Zones[4] != 30 {
		t.Errorf("zones %+v", zones)
	}
	if code := get("/user/hrzones", &zones); code != http.StatusOK || len(zones.Weeks) != models.DashboardWeeks {
		t.Errorf("zones by default: %d, %d weeks", code, len(zones.Weeks))
	}

	var readiness health.Readiness
	if code := get("/user/readiness?days=14", &readiness); code != http.StatusOK {
		t.Fatalf("readiness: %d", code)
	}
	if len(readiness.Series) != 14 || readiness.Today.Load != 80 || readiness.Today.Status != health.NotEnoughData ||
		readiness.Today.RestHR7d == nil || *readiness.Today.RestHR7d != 52 {
		t.Errorf("readiness %+v", readiness.Today)
	}

	for _, path := range []string{"/user/hrzones?weeks=0", "/user/hrzones?weeks=105", "/user/hrzones?weeks=x",
		"/user/readiness?days=6", "/user/readiness?days=366"} {
		if code := get(path, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", path, code)
		}
	}
}

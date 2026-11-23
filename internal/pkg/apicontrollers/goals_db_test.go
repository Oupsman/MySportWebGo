package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestGoals(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User", Timezone: "Europe/Paris"}
	bob := models.Users{Username: "bob", Email: "bob@test.local", Role: "User"}
	for _, u := range []*models.Users{&alice, &bob} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
		db.First(u, u.ID)
	}
	paris, _ := time.LoadLocation("Europe/Paris")
	now := time.Now().In(paris)
	week, _, _ := goalTestWeek(now)
	save := func(u models.Users, sport string, date time.Time, meters float64) {
		t.Helper()
		a := models.Activity{ID: uuid.New(), UserID: u.ID, Title: sport, Sport: sport, Date: date, Distance: meters,
			Duration: 3600}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
	}
	// this week: 30 km of cycling, 10 km of running; the 4 weeks before: 50 km of cycling each, but 2 weeks ago
	// (20 km); bob: 500 km
	save(alice, "cycling", week.Add(time.Hour), 30000)
	save(alice, "running", week.Add(2*time.Hour), 10000)
	for i := 1; i <= 4; i++ {
		meters := 50000.0
		if i == 2 {
			meters = 20000
		}
		save(alice, "cycling", week.AddDate(0, 0, -7*i).Add(time.Hour), meters)
	}
	save(bob, "cycling", week.Add(time.Hour), 500000)

	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/goals", middlewares.IsAuthorized(), GetGoals)
	router.POST("/goals", middlewares.IsAuthorized(), CreateGoal)
	router.POST("/goals/:id", middlewares.IsAuthorized(), UpdateGoal)
	router.DELETE("/goals/:id", middlewares.IsAuthorized(), DeleteGoal)
	call := func(method, path, body string, answer interface{}) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if answer != nil {
			json.Unmarshal(w.Body.Bytes(), answer)
		}
		return w.Code
	}

	// 40 km a week of cycling: 30 so far, reached the weeks 1, 3 and 4 before
	var weekly models.GoalStatus
	if status := call(http.MethodPost, "/goals", `{"metric":"distance","target":40000,"sports":["cycling","cycling"],"period":"week"}`, &weekly); status != http.StatusCreated {
		t.Fatalf("create: %d", status)
	}
	if weekly.Current.Value != 30000 || weekly.Current.Reached || weekly.Current.Start != week.Format(time.DateOnly) ||
		len(weekly.Sports) != 1 || len(weekly.History) != 12 || weekly.Streak != 1 {
		t.Fatalf("weekly: %+v", weekly)
	}
	last := weekly.History[11]
	if last.Value != 50000 || !last.Reached || weekly.History[10].Reached || !weekly.History[9].Reached || weekly.History[0].Value != 0 {
		t.Errorf("history: %+v", weekly.History)
	}
	// all sports: 40 km this week, reached, the streak of the 2 last weeks with it
	var all models.GoalStatus
	call(http.MethodPost, "/goals/"+weekly.ID.String(), `{"metric":"distance","target":40000,"period":"week","name":"Weekly"}`, &all)
	if all.Current.Value != 40000 || all.State != models.GoalReached || all.Streak != 2 || all.Name != "Weekly" {
		t.Errorf("all sports: %+v", all)
	}
	// set while already reached: its period counts as notified
	var stored models.Goal
	db.First(&stored, "id = ?", weekly.ID)
	if stored.NotifiedFor == nil || stored.NotifiedFor.Format(time.DateOnly) != week.Format(time.DateOnly) {
		t.Errorf("notified for: %v", stored.NotifiedFor)
	}

	// a custom period: 2 activities between two dates, the end included
	var custom models.GoalStatus
	body := `{"metric":"activities","target":2,"period":"custom","starts_on":"` + week.AddDate(0, 0, -7).Format(time.DateOnly) +
		`","ends_on":"` + week.AddDate(0, 0, -7).Format(time.DateOnly) + `"}`
	if status := call(http.MethodPost, "/goals", body, &custom); status != http.StatusCreated {
		t.Fatalf("custom: %d", status)
	}
	if custom.Current.Value != 1 || custom.State != models.GoalMissed || custom.StartsOn == nil || len(custom.History) != 0 {
		t.Errorf("custom: %+v", custom)
	}

	for _, bad := range []string{
		`{"metric":"speed","target":1,"period":"week"}`,
		`{"metric":"distance","target":0,"period":"week"}`,
		`{"metric":"distance","target":1,"period":"day"}`,
		`{"metric":"distance","target":1,"period":"week","sports":["quidditch"]}`,
		`{"metric":"distance","target":1,"period":"custom","starts_on":"2026-05-02","ends_on":"2026-05-01"}`,
		`{"metric":"distance","target":1,"period":"custom"}`,
		`{"metric":"activities","target":0.2,"period":"week"}`,
	} {
		if status := call(http.MethodPost, "/goals", bad, nil); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, status)
		}
	}

	var goals []models.GoalStatus
	if status := call(http.MethodGet, "/goals", "", &goals); status != http.StatusOK || len(goals) != 2 || goals[0].ID != weekly.ID {
		t.Fatalf("list: %d %+v", status, goals)
	}
	// bob's goals are not alice's
	bobGoal := models.Goal{UserID: bob.ID, Metric: "distance", Target: 1, Period: "year"}
	if err := db.SaveGoal(&bobGoal, now); err != nil {
		t.Fatal(err)
	}
	if status := call(http.MethodDelete, "/goals/"+bobGoal.ID.String(), "", nil); status != http.StatusNotFound {
		t.Errorf("deleting bob's goal: %d", status)
	}
	if status := call(http.MethodPost, "/goals/"+bobGoal.ID.String(), `{"metric":"distance","target":5,"period":"week"}`, nil); status != http.StatusNotFound {
		t.Errorf("updating bob's goal: %d", status)
	}
	if status := call(http.MethodDelete, "/goals/"+custom.ID.String(), "", nil); status != http.StatusOK {
		t.Errorf("delete: %d", status)
	}
	call(http.MethodGet, "/goals", "", &goals)
	if len(goals) != 1 {
		t.Errorf("after the delete: %d goals", len(goals))
	}
}

// goalTestWeek returns the Monday of the week of now, at midnight
func goalTestWeek(now time.Time) (time.Time, time.Time, time.Time) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7)), day, now
}

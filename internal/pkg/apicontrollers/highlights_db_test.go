package apicontrollers

import (
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// the activity page and the dashboard tell the new bests of the user, never of another user
func TestHighlightsAPI(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	vars.Domain = "https://sport.example.com"
	client := twoFactorRouter(t, db)
	client.router.GET("/activity/:id/highlights", middlewares.IsAuthorized(models.ActivitiesRead), GetActivityHighlights)
	client.router.GET("/dashboard", middlewares.IsAuthorized(models.ActivitiesRead), Dashboard)
	users := createUsers(t, db, "password", "ann", "ben")
	ann, ben := users[0], users[1]

	five := func(seconds float64) types.BestEffortArray {
		return types.BestEffortArray{{Sport: "running", Kind: types.EffortDistance, Target: 5000, Value: seconds}}
	}
	create := func(user models.Users, daysAgo int, seconds float64) uuid.UUID {
		t.Helper()
		a := models.Activity{ID: uuid.New(), UserID: user.ID, Title: "run", Sport: "running",
			Date: time.Now().AddDate(0, 0, -daysAgo), BestEfforts: five(seconds)}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	create(ann, 3, 1500)
	best := create(ann, 1, 1400)
	bens := create(ben, 2, 1300)

	cookie := client.loginCookie("ann", "password")
	status, answer := adminRequest(client, http.MethodGet, "/activity/"+best.String()+"/highlights", cookie, "", "")
	list, _ := answer["highlights"].([]interface{})
	if status != http.StatusOK || len(list) != 1 {
		t.Fatalf("highlights: %d %v", status, answer)
	}
	if h := list[0].(map[string]interface{}); h["rank"] != float64(1) || h["current"] != true || h["previous"] != float64(1500) {
		t.Errorf("highlight %v", h)
	}
	// another user's activity: nothing, as if it had none
	status, answer = adminRequest(client, http.MethodGet, "/activity/"+bens.String()+"/highlights", cookie, "", "")
	if list, _ := answer["highlights"].([]interface{}); status != http.StatusOK || len(list) != 0 {
		t.Errorf("another user's activity: %d %v", status, answer)
	}
	if status, _ := adminRequest(client, http.MethodGet, "/activity/nope/highlights", cookie, "", ""); status != http.StatusBadRequest {
		t.Errorf("not an id: %d", status)
	}

	status, answer = adminRequest(client, http.MethodGet, "/dashboard", cookie, "", "")
	if list, _ := answer["highlights"].([]interface{}); status != http.StatusOK || len(list) != 1 {
		t.Errorf("dashboard highlights: %d %v", status, answer["highlights"])
	}
}

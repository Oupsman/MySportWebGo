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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func TestDashboardLayout(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User"}
	bob := models.Users{Username: "bob", Email: "bob@test.local", Role: "User"}
	for _, u := range []*models.Users{&alice, &bob} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
		db.First(u, u.ID)
	}
	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/layout", middlewares.IsAuthorized(), GetDashboardLayout)
	router.PUT("/layout", middlewares.IsAuthorized(), SetDashboardLayout)
	call := func(method, body string) (int, []models.DashboardWidget, bool) {
		t.Helper()
		req := httptest.NewRequest(method, "/layout", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var answer struct{ Widgets []models.DashboardWidget }
		json.Unmarshal(w.Body.Bytes(), &answer)
		return w.Code, answer.Widgets, strings.Contains(w.Body.String(), `"widgets":null`)
	}

	// the default layout: null
	if status, _, null := call(http.MethodGet, ""); status != http.StatusOK || !null {
		t.Fatalf("default: %d", status)
	}
	layout := `{"widgets":[{"id":"calendar","w":4,"h":0},{"id":"weekly","w":8,"h":420,"hidden":true}]}`
	if status, _, _ := call(http.MethodPut, layout); status != http.StatusOK {
		t.Fatalf("save: %d", status)
	}
	status, widgets, _ := call(http.MethodGet, "")
	want := []models.DashboardWidget{{ID: "calendar", W: 4}, {ID: "weekly", W: 8, H: 420, Hidden: true}}
	if status != http.StatusOK || !reflect.DeepEqual(widgets, want) {
		t.Fatalf("saved before the grid: %d %+v", status, widgets)
	}
	// on the grid: a widget as high as the two beside it
	grid := `{"widgets":[{"id":"readiness","x":8,"y":0,"w":4,"h":20},{"id":"calendar","x":0,"y":0,"w":8,"h":10},` +
		`{"id":"weekly","x":0,"y":10,"w":8,"h":10,"hidden":true}]}`
	if status, _, _ := call(http.MethodPut, grid); status != http.StatusOK {
		t.Fatalf("save on the grid: %d", status)
	}
	at := func(n int) *int { return &n }
	status, widgets, _ = call(http.MethodGet, "")
	want = []models.DashboardWidget{
		{ID: "readiness", X: at(8), Y: at(0), W: 4, H: 20},
		{ID: "calendar", X: at(0), Y: at(0), W: 8, H: 10},
		{ID: "weekly", X: at(0), Y: at(10), W: 8, H: 10, Hidden: true},
	}
	if status != http.StatusOK || !reflect.DeepEqual(widgets, want) {
		t.Fatalf("saved on the grid: %d %+v", status, widgets)
	}
	// bob's dashboard stays the default one
	if got, err := db.GetDashboardLayout(bob.ID); err != nil || got != nil {
		t.Errorf("bob: %+v %v", got, err)
	}
	for _, bad := range []string{
		`{"widgets":[{"id":"calendar","w":0}]}`,
		`{"widgets":[{"id":"calendar","w":13}]}`,
		`{"widgets":[{"id":"calendar","w":4,"h":-1}]}`,
		`{"widgets":[{"id":"Calendar!","w":4}]}`,
		`{"widgets":[{"id":"calendar","w":4},{"id":"calendar","w":6}]}`,
		`{"widgets":[{"id":"calendar","x":9,"y":0,"w":4,"h":10}]}`,
		`{"widgets":[{"id":"calendar","x":-1,"y":0,"w":4,"h":10}]}`,
		`{"widgets":[{"id":"calendar","x":0,"y":-1,"w":4,"h":10}]}`,
		`{"widgets":[{"id":"calendar","x":0,"y":0,"w":4,"h":0}]}`,
		`{"widgets":[{"id":"calendar","x":0,"y":0,"w":4,"h":101}]}`,
		`{"widgets":[{"id":"calendar","x":0,"w":4,"h":10}]}`,
		`{"widgets":[{"id":"calendar","x":0,"y":0,"w":4,"h":10},{"id":"weekly","w":8,"h":0}]}`,
		`{"widgets":[{"id":"weekly","w":8,"h":0},{"id":"calendar","x":0,"y":0,"w":4,"h":10}]}`,
		`not json`,
	} {
		if status, _, _ := call(http.MethodPut, bad); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, status)
		}
	}
	// refused: the saved layout is kept
	if _, widgets, _ := call(http.MethodGet, ""); len(widgets) != 3 {
		t.Errorf("after the refused ones: %+v", widgets)
	}
	// back to the default layout
	if status, _, _ := call(http.MethodPut, `{"widgets":null}`); status != http.StatusOK {
		t.Fatalf("reset: %d", status)
	}
	if _, _, null := call(http.MethodGet, ""); !null {
		t.Error("reset: not the default layout")
	}
}

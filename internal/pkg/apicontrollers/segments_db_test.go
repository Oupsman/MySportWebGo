package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// the segments through the API: created from an activity of the user only, read, renamed and deleted by
// their user only
func TestSegmentsAPI(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	users := createUsers(t, db, "password", "rider", "other")
	rider, other := users[0], users[1]

	// a ride 1 km east, a point every 5 m at 5 m/s
	activity := models.Activity{ID: uuid.New(), UserID: rider.ID, Sport: "cycling", Title: "Morning ride", Date: time.Now()}
	for d := 0.0; d <= 1000; d += 5 {
		activity.GpsPoints = append(activity.GpsPoints, types.GpsPoint{Lat: 45.18,
			Lon: 5.72 + d/(6371000*math.Cos(45.18*math.Pi/180))*180/math.Pi})
		activity.GpsTimeStamps = append(activity.GpsTimeStamps, d/5)
	}
	last := activity.GpsPoints[len(activity.GpsPoints)-1]
	activity.GpsBounds = types.GpsPointArray{activity.GpsPoints[0], last}
	if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/segments", middlewares.IsAuthorized(), CreateSegment)
	router.GET("/segments", middlewares.IsAuthorized(), GetSegments)
	router.GET("/segments/:id", middlewares.IsAuthorized(), GetSegment)
	router.POST("/segments/:id", middlewares.IsAuthorized(), RenameSegment)
	router.DELETE("/segments/:id", middlewares.IsAuthorized(), DeleteSegment)
	router.GET("/activity/:id/segments", middlewares.IsAuthorized(), GetActivitySegments)

	send := func(user models.Users, method, path, body string) (int, map[string]json.RawMessage) {
		t.Helper()
		token, _, err := utils.NewSessionToken(user.ID, user.UUID, user.Role, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		answer := map[string]json.RawMessage{}
		_ = json.Unmarshal(w.Body.Bytes(), &answer)
		return w.Code, answer
	}
	create := func(user models.Users, start, end int, name string) (int, map[string]json.RawMessage) {
		return send(user, http.MethodPost, "/segments",
			fmt.Sprintf(`{"activity_id": %q, "start": %d, "end": %d, "name": %q}`, activity.ID, start, end, name))
	}

	if code, _ := create(other, 20, 180, "theirs"); code != http.StatusNotFound {
		t.Errorf("from another user's activity: %d", code)
	}
	for _, c := range []struct {
		start, end int
		name       string
	}{{0, 10, "too short"}, {150, 20, "backwards"}, {0, 9999, "past the end"}, {0, 100, "  "}} {
		if code, answer := create(rider, c.start, c.end, c.name); code != http.StatusBadRequest {
			t.Errorf("%q: %d %s", c.name, code, answer["error"])
		}
	}
	code, answer := create(rider, 20, 180, "  The flat  ")
	if code != http.StatusCreated || string(answer["efforts"]) != "1" {
		t.Fatalf("create: %d %v", code, answer)
	}
	var segment models.Segment
	if err := json.Unmarshal(answer["segment"], &segment); err != nil || segment.Name != "The flat" ||
		math.Abs(segment.Distance-800) > 2 {
		t.Fatalf("segment %+v (%v)", segment, err)
	}
	path := "/segments/" + segment.ID.String()

	code, answer = send(rider, http.MethodGet, path, "")
	var efforts []models.RankedEffort
	if code != http.StatusOK || json.Unmarshal(answer["efforts"], &efforts) != nil || len(efforts) != 1 ||
		efforts[0].Rank != 1 || efforts[0].Elapsed != 160 || efforts[0].ActivityTitle != "Morning ride" {
		t.Errorf("get: %d %+v", code, efforts)
	}
	if code, answer = send(rider, http.MethodGet, "/activity/"+activity.ID.String()+"/segments", ""); code != http.StatusOK ||
		!strings.Contains(string(answer["efforts"]), `"segment_name":"The flat"`) {
		t.Errorf("the passes of the activity: %d %s", code, answer["efforts"])
	}
	if code, answer = send(rider, http.MethodGet, "/segments", ""); !strings.Contains(string(answer["segments"]), `"efforts":1`) {
		t.Errorf("list: %d %s", code, answer["segments"])
	}
	if code, answer = send(other, http.MethodGet, "/segments", ""); string(answer["segments"]) != "[]" {
		t.Errorf("another user's list: %d %s", code, answer["segments"])
	}
	for _, c := range []struct{ method, body string }{{http.MethodGet, ""}, {http.MethodPost, `{"name": "mine"}`}, {http.MethodDelete, ""}} {
		if code, _ := send(other, c.method, path, c.body); code != http.StatusNotFound {
			t.Errorf("another user, %s: %d", c.method, code)
		}
	}
	if code, _ := send(rider, http.MethodPost, path, `{"name": ""}`); code != http.StatusBadRequest {
		t.Errorf("an empty name: %d", code)
	}
	if code, answer := send(rider, http.MethodPost, path, `{"name": "Flat out"}`); code != http.StatusOK || string(answer["name"]) != `"Flat out"` {
		t.Errorf("rename: %d %v", code, answer)
	}
	if code, _ := send(rider, http.MethodDelete, path, ""); code != http.StatusOK {
		t.Errorf("delete: %d", code)
	}
	if code, _ := send(rider, http.MethodGet, path, ""); code != http.StatusNotFound {
		t.Errorf("after delete: %d", code)
	}
}

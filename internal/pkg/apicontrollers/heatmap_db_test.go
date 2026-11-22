package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
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
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// decodePolyline decodes Google's encoded polyline format, precision 5
func decodePolyline(s string) []types.GpsPoint {
	var points []types.GpsPoint
	var lat, lon int64
	for i := 0; i < len(s); {
		var deltas [2]int64
		for k := range deltas {
			var result, shift uint64
			for {
				b := uint64(s[i]) - 63
				i++
				result |= (b & 0x1f) << shift
				shift += 5
				if b < 0x20 {
					break
				}
			}
			if result&1 != 0 {
				deltas[k] = ^int64(result >> 1)
			} else {
				deltas[k] = int64(result >> 1)
			}
		}
		lat, lon = lat+deltas[0], lon+deltas[1]
		points = append(points, types.GpsPoint{Lat: float64(lat) / 1e5, Lon: float64(lon) / 1e5})
	}
	return points
}

func TestHeatmap(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User", Timezone: "Europe/Paris", SecurityDistance: 500}
	bob := models.Users{Username: "bob", Email: "bob@test.local", Role: "User"}
	for _, u := range []*models.Users{&alice, &bob} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
		db.First(u, u.ID)
	}
	trace := func(meters float64) types.GpsPointArray {
		var points types.GpsPointArray
		for d := 0.0; d <= meters; d += 100 {
			points = append(points, types.GpsPoint{Lat: 45 + d/111195, Lon: 6})
		}
		return points
	}
	save := func(u models.Users, sport string, date time.Time, points types.GpsPointArray) models.Activity {
		t.Helper()
		a := models.Activity{ID: uuid.New(), UserID: u.ID, Title: sport, Sport: sport, Date: date, Distance: 2000,
			SimplifiedGpsPoints: points}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a
	}
	// a ride at 00:30 in Paris on May 2 (22:30 UTC on May 1), a run, a swim without GPS, bob's ride
	ride := save(alice, "cycling", time.Date(2026, 5, 1, 22, 30, 0, 0, time.UTC), trace(2000))
	run := save(alice, "running", time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC), trace(3000))
	save(alice, "swimming", time.Date(2026, 5, 11, 8, 0, 0, 0, time.UTC), nil)
	save(bob, "cycling", time.Date(2026, 5, 3, 8, 0, 0, 0, time.UTC), trace(5000))

	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/heatmap", middlewares.IsAuthorized(), Heatmap)
	get := func(query, etag string) (int, []heatmapActivity, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/heatmap"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var answer struct{ Activities []heatmapActivity }
		json.Unmarshal(w.Body.Bytes(), &answer)
		return w.Code, answer.Activities, w.Header().Get("ETag")
	}

	status, all, etag := get("", "")
	if status != http.StatusOK || len(all) != 2 || all[0].ID != ride.ID || all[1].ID != run.ID || etag == "" {
		t.Fatalf("all: %d %+v", status, all)
	}
	if points := decodePolyline(all[0].Trace); len(points) != 21 || points[0] != (types.GpsPoint{Lat: 45, Lon: 6}) {
		t.Errorf("ride trace: %d points, first %v", len(points), points[0])
	}
	if all[0].Date != "2026-05-02" || all[0].Bounds[0] != 45 || all[0].Bounds[1] != 6 {
		t.Errorf("ride: %+v", all[0])
	}
	// nothing changed: not modified
	if status, _, _ := get("", etag); status != http.StatusNotModified {
		t.Errorf("same etag: %d", status)
	}
	// a filter is another version
	if status, runs, other := get("?sport=running", etag); status != http.StatusOK || len(runs) != 1 || runs[0].ID != run.ID || other == etag {
		t.Errorf("runs: %d %+v", status, runs)
	}
	if _, both, _ := get("?sport=running&sport=cycling", ""); len(both) != 2 {
		t.Errorf("two sports: %d", len(both))
	}
	// the days of the user: the ride is on May 2 in Paris
	if _, may2, _ := get("?from=2026-05-02&to=2026-05-02", ""); len(may2) != 1 || may2[0].ID != ride.ID {
		t.Errorf("May 2: %+v", may2)
	}
	if _, before, _ := get("?to=2026-05-01", ""); len(before) != 0 {
		t.Errorf("until May 1: %+v", before)
	}
	// the ends hidden: 500 m less at each end
	_, hidden, _ := get("?hide_ends=true", "")
	if len(hidden) != 2 {
		t.Fatalf("hidden: %+v", hidden)
	}
	if points := decodePolyline(hidden[0].Trace); points[0].Lat < 45+490/111195.0 || points[len(points)-1].Lat > 45+1510/111195.0 {
		t.Errorf("ride with its ends hidden: %v ... %v", points[0], points[len(points)-1])
	}
	// a new activity: a new version
	save(alice, "cycling", time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC), trace(1000))
	if status, after, _ := get("", etag); status != http.StatusOK || len(after) != 3 {
		t.Errorf("after a new activity: %d %d", status, len(after))
	}
	for _, bad := range []string{"?sport=biking", "?from=may"} {
		if status, _, _ := get(bad, ""); status != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, status)
		}
	}
}

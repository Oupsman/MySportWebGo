package activityService

import (
	"MySportWeb/internal/pkg/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"os"
	"strings"
	"sync"
	"testing"
)

// Integration test of the routes, it needs a PostgreSQL database whose name contains "test":
// MYSPORTWEB_TEST_DSN="host=localhost port=5432 user=test dbname=mysport_test sslmode=disable"
// WARNING: the activities, routes and users tables are emptied.

// files of the same route, and of another one (see TestSimilarFitFiles)
var (
	sameRouteFiles  = []string{"e9272ebc-f40f-485e-9918-bc859012e636", "9cbbea97-8eef-4815-95dd-ec255255ad98", "cb369766-6433-453c-9182-f625866543c0", "8393322b-5f34-43c9-a152-82b6955adb2f", "1e893571-c023-431d-8443-c42236c415e7", "9c0d723a-af25-4c83-b04f-26f15b5c4249", "e82038cd-25cf-459e-8d45-bc76ba967449"}
	otherRouteFile  = "a9690755-6587-4995-abca-2f131f86f1b2"
	backfilledFiles = []string{"278c2cd7-a8c5-4cda-9f10-abaa25335e50", "91bc86d3-29e0-4cde-8294-37dc10ce70b5"}
)

func testDB(t *testing.T) *models.DB {
	for _, name := range append(append(sameRouteFiles, otherRouteFile), backfilledFiles...) {
		if _, err := os.Stat("../../prototypes/" + name + ".fit"); err != nil {
			t.Skip("FIT files missing in prototypes/")
		}
	}
	return emptyDB(t)
}

// emptyDB returns the test database emptied, for the tests that don't read FIT files
func emptyDB(t *testing.T) *models.DB {
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
	if err := db.Exec("TRUNCATE activities, routes, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func importFit(t *testing.T, name string, user models.Users, equipment models.Equipments) models.Activity {
	activity, err := SumAnalyze("../../prototypes/"+name+".fit", user, equipment)
	if err != nil {
		t.Fatal(err)
	}
	activity.ID = uuid.New()
	return activity
}

func countRoutes(t *testing.T, db *models.DB) []models.Route {
	var routes []models.Route
	if err := db.Order("activity_count desc").Find(&routes).Error; err != nil {
		t.Fatal(err)
	}
	return routes
}

func TestRoutesDatabase(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "routes", Email: "routes@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID), IsDefault: true}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}

	// concurrent imports of the same route must not create several routes
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, len(sameRouteFiles))
	for i, name := range sameRouteFiles {
		activity := importFit(t, name, user, equipment)
		ids[i] = activity.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := CreateActivity(db, &activity, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	other := importFit(t, otherRouteFile, user, equipment)
	if err := CreateActivity(db, &other, nil); err != nil {
		t.Fatal(err)
	}

	routes := countRoutes(t, db)
	if len(routes) != 2 || routes[0].ActivityCount != len(sameRouteFiles) || routes[1].ActivityCount != 1 {
		t.Fatalf("expected 2 routes with %d and 1 activities, got %+v", len(sameRouteFiles), routes)
	}

	activity, err := db.GetActivity(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if activity.RouteID == nil || *activity.RouteID != routes[0].ID {
		t.Fatalf("activity is not linked to its route: %v", activity.RouteID)
	}
	similar, err := db.GetActivitiesByRoute(*activity.RouteID, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(similar) != len(sameRouteFiles)-1 {
		t.Fatalf("expected %d similar activities, got %d", len(sameRouteFiles)-1, len(similar))
	}

	// deleting updates the count, and removes the route once empty
	if err := db.DeleteActivity(ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteActivity(other.ID); err != nil {
		t.Fatal(err)
	}
	routes = countRoutes(t, db)
	if len(routes) != 1 || routes[0].ActivityCount != len(sameRouteFiles)-1 {
		t.Fatalf("expected 1 route with %d activities, got %+v", len(sameRouteFiles)-1, routes)
	}

	// activities imported before routes existed: no route, no simplified trace
	var backfilled []uuid.UUID
	for _, name := range backfilledFiles {
		activity := importFit(t, name, user, equipment)
		activity.SimplifiedGpsPoints = nil
		if err := db.CreateActivity(&activity); err != nil {
			t.Fatal(err)
		}
		backfilled = append(backfilled, activity.ID)
	}
	if err := BackfillRoutes(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	var routeIDs []uuid.UUID
	for _, id := range backfilled {
		activity, err := db.GetActivityForRoute(id)
		if err != nil {
			t.Fatal(err)
		}
		if activity.RouteID == nil || len(activity.SimplifiedGpsPoints) < 2 {
			t.Fatalf("activity %s was not backfilled", id)
		}
		routeIDs = append(routeIDs, *activity.RouteID)
	}
	if routeIDs[0] != routeIDs[1] {
		t.Fatal("backfilled activities should share a route")
	}
	if routes = countRoutes(t, db); len(routes) != 2 {
		t.Fatalf("expected 2 routes after backfill, got %d", len(routes))
	}
	// running it again is a no-op
	if err := BackfillRoutes(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if routes = countRoutes(t, db); len(routes) != 2 || routes[1].ActivityCount != 2 {
		t.Fatalf("second backfill changed the routes: %+v", routes)
	}
}

// routes split by a poor reference are merged, with their activities, count and commute flag
func TestMergeRoutesDatabase(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "merge", Email: "merge@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for _, name := range sameRouteFiles[:4] {
		activity := importFit(t, name, user, models.Equipments{})
		if err := CreateActivity(db, &activity, nil); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, activity.ID)
	}
	other := importFit(t, otherRouteFile, user, models.Equipments{})
	if err := CreateActivity(db, &other, nil); err != nil {
		t.Fatal(err)
	}
	routes := countRoutes(t, db)
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	kept := routes[0]

	// a second route of the same path, whose reference doesn't match, flagged as a commute
	first, err := db.GetActivityForRoute(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	first.Distance *= 1.3
	split := NewRoute(*first)
	split.ActivityCount = 2
	if err := db.CreateRoute(&split); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Activity{}).Where("id IN ?", ids[2:]).Update("route_id", split.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetRouteCommute(split.ID, true); err != nil {
		t.Fatal(err)
	}

	if err := MergeRoutes(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	routes = countRoutes(t, db)
	if len(routes) != 2 || routes[0].ID != kept.ID || routes[0].ActivityCount != 4 || routes[1].ActivityCount != 1 {
		t.Fatalf("expected the routes of 4 and 1 activities, got %+v", routes)
	}
	if !routes[0].IsCommute {
		t.Error("the merged route lost the commute flag")
	}
	for _, id := range ids {
		activity, err := db.GetActivity(id)
		if err != nil {
			t.Fatal(err)
		}
		if activity.RouteID == nil || *activity.RouteID != kept.ID || !activity.IsCommute {
			t.Errorf("activity %s: route %v, commute %v", id, activity.RouteID, activity.IsCommute)
		}
	}

	// running it again is a no-op
	if err := MergeRoutes(db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if again := countRoutes(t, db); len(again) != 2 || again[0].ActivityCount != 4 {
		t.Fatalf("second merge changed the routes: %+v", again)
	}
}

package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/staticmap"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/services/activityService"
	"MySportWeb/services/activityService/convert/converttest"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// a zip of the given entries
func zipOf(t *testing.T, entries map[string][]byte, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(entries[name])
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOpenExportRefuses(t *testing.T) {
	dir := t.TempDir()
	open := func(content []byte) (*exportZip, error) {
		path := dir + "/export.zip"
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		e, err := openExport(path)
		if e != nil {
			e.Close()
		}
		return e, err
	}
	manifest := []byte(`{"format": 2, "app_version": "1.0.0"}`)
	for name, content := range map[string][]byte{
		"not a zip":     []byte("hello"),
		"not an export": zipOf(t, map[string][]byte{"notes.txt": []byte("x")}, "notes.txt"),
		"newer format": zipOf(t, map[string][]byte{"manifest.json": []byte(`{"format": 99, "app_version": "9.0.0"}`)},
			"manifest.json"),
		"no format":         zipOf(t, map[string][]byte{"manifest.json": []byte(`{}`)}, "manifest.json"),
		"invalid manifest":  zipOf(t, map[string][]byte{"manifest.json": []byte(`{`)}, "manifest.json"),
		"twice the same":    zipOf(t, map[string][]byte{"manifest.json": manifest}, "manifest.json", "manifest.json"),
		"too large picture": zipOf(t, map[string][]byte{"manifest.json": manifest, "pictures/a.jpg": make([]byte, 16<<20)}, "manifest.json", "pictures/a.jpg"),
	} {
		_, err := open(content)
		var invalid *invalidExportError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// the entries it doesn't know are ignored, never read: a sub-directory, a path out of the zip
	e, err := open(zipOf(t, map[string][]byte{"profile.json": []byte(`{}`), "activities/a.json": []byte(`{}`)},
		"profile.json", "activities/a.json", "../evil.json", "files/../../evil.fit", "__MACOSX/._profile.json",
		"activities/sub/b.json"))
	if err != nil {
		t.Fatal(err)
	}
	if e.format != 1 || len(e.activities) != 1 || len(e.files) != 2 {
		t.Errorf("format %d, activities %v, files %d", e.format, e.activities, len(e.files))
	}
}

// a JPEG of the equipment pictures
func testPicture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := 0; x < 40; x++ {
		img.Set(x, x%30, color.RGBA{200, 0, 0, 255})
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An account exported, then imported into an empty one: everything the user set comes back; imported again,
// nothing changes
func TestAccountImportRoundTrip(t *testing.T) {
	db := testDB(t)
	// the routes have no foreign key to their user: the ones of the users of the tests before stay, and the
	// users get the same ids again
	if err := db.Exec("TRUNCATE routes").Error; err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	saved := activityService.MapTiles
	activityService.MapTiles = staticmap.NewTiles(t.TempDir(), "") // offline: blank tiles
	t.Cleanup(func() { activityService.MapTiles = saved })
	vars.SecretKey = "testsecret"
	ctx := context.Background()
	newUser := func(name string) models.Users {
		t.Helper()
		user := models.Users{Username: name, Email: name + "@test.local", Role: "User"}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(&user, user.ID).Error; err != nil {
			t.Fatal(err)
		}
		return user
	}
	upload := func(user models.Users, content []byte, name, sport string) models.Activity {
		t.Helper()
		path, err := activityService.CreateImportFile(user.UUID)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(path, content, 0o660)
		activity, err := activityService.ImportFile(ctx, db, user, path, name, activityService.ImportOptions{Sport: sport})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return activity
	}
	check := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	// alice has one of everything
	alice := newUser("alice")
	check("profile", db.UpdateUserFields(alice.ID, map[string]interface{}{"max_hr": 190, "home_town": "Grenoble"}))
	check("layout", db.SetDashboardLayout(alice.ID, []models.DashboardWidget{{ID: "calendar", W: 6, H: 2}}))
	day := time.Date(2025, 5, 1, 8, 0, 0, 0, time.UTC)
	bike := models.Equipments{Name: "Gravel", Brand: "Canyon", EquipmentModel: "Grizl", UserID: int(alice.ID),
		InitialMileage: 1200, DefaultSports: []string{"cycling"}, IsCommute: true}
	check("bike", db.CreateEquipment(&bike))
	picture, err := activityService.SaveEquipmentPicture(alice.UUID, bike.ID, testPicture(t))
	check("picture", err)
	check("picture", db.SetEquipmentPicture(bike.ID, picture))
	chain := models.MaintenanceReminder{EquipmentID: bike.ID, Name: "chain", EveryKm: 3000, OnlyWhenUsed: true, StartsAt: day}
	check("reminder", db.CreateReminder(&chain))
	check("service", db.CreateService(&models.Services{EquipmentID: bike.ID, Date: day, Mileage: 100,
		Description: "new chain", ReminderIDs: []uuid.UUID{chain.ID}}))
	check("health", db.Create(&models.HealthData{UserID: alice.ID, Date: day, Weight: 70, RestHR: 50}).Error)
	from, to := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	check("goal", db.Create(&models.Goal{UserID: alice.ID, Name: "june", Metric: "distance", Target: 500000,
		Period: "custom", Sports: models.SportList{"cycling"}, StartsOn: &from, EndsOn: &to}).Error)

	ride := upload(alice, converttest.GPX(false), "Morning_Ride.gpx", "cycling")
	run := upload(alice, converttest.TCX(), "run.tcx", "")
	if run.DeviceID == nil {
		t.Fatal("the run has no device")
	}
	label := "My watch"
	check("device", db.UpdateDevice(alice.ID, *run.DeviceID, &label, nil))
	// the ride: trimmed, renamed, private, a commute on a commute route
	stored, err := db.GetActivity(ride.ID)
	check("ride", err)
	trimmed, err := activityService.TrimActivity(db, stored, 400, zerolog.Nop())
	check("trim", err)
	trimmed.Title, trimmed.Visibility, trimmed.IsCommute = "Col du test", 2, true
	trimmed.Equipment = models.Equipments{}
	check("ride", db.UpdateActivity(trimmed))
	if trimmed.RouteID == nil {
		t.Fatal("the ride has no route")
	}
	_, err = db.SetRouteCommute(*trimmed.RouteID, true)
	check("route", err)
	track, err := db.GetSegmentTrack(ride.ID)
	check("track", err)
	segment, err := activityService.NewSegment(track, 10, 250, "the climb")
	check("segment", err)
	check("segment", db.CreateSegment(&segment))
	_, err = activityService.MatchSegmentActivities(db, segment, activityService.MatchCreate)
	check("passes", err)

	// her settings as the export reads them: the user of the request
	alice, err = db.GetUser(uint64(alice.ID))
	check("alice", err)
	var export bytes.Buffer
	check("export", activityService.ExportUser(ctx, db, alice, &export))

	// bob imports it, with the profile, through the API
	bob := newUser("bob")
	token, _, err := utils.NewSessionToken(bob.ID, bob.UUID, bob.Role, time.Now())
	check("token", err)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.Use(middlewares.LimitBody(1<<20, map[string]int64{"/user/import": 100 << 20}))
	router.POST("/user/import", middlewares.IsAuthorized(), ImportAccount)
	router.GET("/user/import", middlewares.IsAuthorized(), GetAccountImport)
	send := func(content []byte, profile bool) (int, models.AccountImport) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if profile {
			form.WriteField("profile", "true")
		}
		part, _ := form.CreateFormFile("file", "export.zip")
		part.Write(content)
		form.Close()
		req := httptest.NewRequest(http.MethodPost, "/user/import", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var imp models.AccountImport
		json.Unmarshal(w.Body.Bytes(), &imp)
		if w.Code == http.StatusAccepted && !App.Wait(time.Minute) {
			t.Fatal("the import doesn't end")
		}
		return w.Code, imp
	}
	latest := func() models.AccountImport {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/user/import", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var imp models.AccountImport
		if err := json.Unmarshal(w.Body.Bytes(), &imp); err != nil || w.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", w.Code, w.Body.String())
		}
		return imp
	}

	if code, _ := send([]byte("not a zip"), false); code != http.StatusBadRequest {
		t.Errorf("not a zip: %d", code)
	}
	code, started := send(export.Bytes(), true)
	if code != http.StatusAccepted || started.Total != 2 || !started.WithProfile {
		t.Fatalf("import: %d %+v", code, started)
	}
	imp := latest()
	if imp.Status != models.ImportDone || imp.Done != 2 || imp.Report == nil {
		t.Fatalf("import: %+v", imp)
	}
	counts := imp.Report.Counts
	for kind, created := range map[string]int{"activities": 2, "equipments": 1, "pictures": 1, "reminders": 1,
		"services": 1, "health": 1, "goals": 1, "segments": 1, "devices": 2, "profile": 1} {
		if counts[kind] == nil || counts[kind].Created != created || counts[kind].Skipped != 0 {
			t.Errorf("%s: %+v", kind, counts[kind])
		}
	}
	if len(imp.Report.Problems) > 0 {
		t.Errorf("problems: %+v", imp.Report.Problems)
	}
	var row models.AccountImport
	check("import row", db.First(&row, "id = ?", imp.ID).Error)
	if _, err := os.Stat(row.ZipPath); row.ZipPath == "" || !os.IsNotExist(err) {
		t.Errorf("the zip %q stays: %v", row.ZipPath, err)
	}

	// bob's account, as alice's
	user, err := db.GetUser(uint64(bob.ID))
	check("bob", err)
	if user.MaxHR != 190 || user.HomeTown != "Grenoble" || user.Email != "bob@test.local" {
		t.Errorf("profile: %d %s %s", user.MaxHR, user.HomeTown, user.Email)
	}
	if layout, _ := db.GetDashboardLayout(bob.ID); len(layout) != 1 || layout[0].ID != "calendar" {
		t.Errorf("layout: %+v", layout)
	}
	equipments, err := db.GetEquipmentsStats(bob.ID)
	check("equipments", err)
	if len(equipments) != 1 || equipments[0].Name != "Gravel" || equipments[0].InitialMileage != 1200 ||
		!equipments[0].IsCommute || equipments[0].Picture == "" || len(equipments[0].DefaultSports) != 1 ||
		len(equipments[0].Reminders) != 1 || equipments[0].Reminders[0].EveryKm != 3000 {
		t.Fatalf("equipments: %+v", equipments)
	}
	gravel := equipments[0]
	services, err := db.GetServices(gravel.ID)
	check("services", err)
	if len(services) != 1 || len(services[0].ReminderIDs) != 1 || services[0].ReminderIDs[0] != gravel.Reminders[0].ID {
		t.Errorf("services: %+v", services)
	}
	var activities []models.Activity
	check("activities", db.Where("user_id = ?", bob.ID).Order("sport").Find(&activities).Error)
	if len(activities) != 2 {
		t.Fatalf("%d activities", len(activities))
	}
	bobRide, bobRun := activities[0], activities[1]
	if bobRide.Title != "Col du test" || bobRide.Visibility != 2 || !bobRide.IsCommute || bobRide.TrimEnd != 400 ||
		bobRide.EquipmentID == nil || *bobRide.EquipmentID != gravel.ID || bobRide.Filename != "Morning_Ride.gpx" {
		t.Errorf("ride: %q %d %v %v %v %s", bobRide.Title, bobRide.Visibility, bobRide.IsCommute, bobRide.TrimEnd,
			bobRide.EquipmentID, bobRide.Filename)
	}
	if bobRide.RouteID == nil {
		t.Error("the ride has no route")
	} else if route, err := db.GetRoute(*bobRide.RouteID); err != nil || !route.IsCommute {
		t.Errorf("route: %+v %v", route, err)
	}
	if c := counts["commute routes"]; c == nil || c.Created != 1 {
		t.Errorf("commute routes: %+v", c)
	}
	if bobRun.DeviceID == nil {
		t.Fatal("the run has no device")
	}
	if label, _ := db.DeviceLabel(*bobRun.DeviceID); label != "My watch" {
		t.Errorf("device label %q", label)
	}
	if health, _ := db.GetHealthDatas(bob.ID); len(health) != 1 || health[0].RestHR != 50 {
		t.Errorf("health: %+v", health)
	}
	var goals []models.Goal
	db.Where("user_id = ?", bob.ID).Find(&goals)
	if len(goals) != 1 || goals[0].StartsOn == nil || !goals[0].StartsOn.Equal(from) {
		t.Errorf("goals: %+v", goals)
	}
	var segments []models.Segment
	db.Where("user_id = ?", bob.ID).Find(&segments)
	if len(segments) != 1 || segments[0].Name != "the climb" || segments[0].SourceActivityID == nil ||
		*segments[0].SourceActivityID != bobRide.ID {
		t.Fatalf("segments: %+v", segments)
	}
	if efforts, _ := db.GetSegmentEfforts(bob.ID, segments[0].ID); len(efforts) != 1 {
		t.Errorf("passes: %+v", efforts)
	}

	// again: everything is there already
	if code, _ := send(export.Bytes(), false); code != http.StatusAccepted {
		t.Fatalf("again: %d", code)
	}
	again := latest()
	if again.Status != models.ImportDone || again.ID == imp.ID {
		t.Fatalf("again: %+v", again)
	}
	for kind, existing := range map[string]int{"activities": 2, "equipments": 1, "health": 1, "goals": 1,
		"segments": 1, "devices": 2} {
		c := again.Report.Counts[kind]
		if c == nil || c.Created != 0 || c.Existing != existing {
			t.Errorf("again, %s: %+v", kind, c)
		}
	}
	var count int64
	db.Model(&models.Activity{}).Where("user_id = ?", bob.ID).Count(&count)
	if count != 2 {
		t.Errorf("%d activities after the second import", count)
	}
	if again.Error != "" {
		t.Errorf("error: %s", again.Error)
	}
}

// An import a stop of the server interrupted runs again at the start; one cancelled stops, its zip removed
func TestAccountImportResumeAndCancel(t *testing.T) {
	db := testDB(t)
	t.Chdir(t.TempDir())
	vars.SecretKey = "testsecret"
	user := models.Users{Username: "carol", Email: "carol@test.local", Role: "User"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	export := zipOf(t, map[string][]byte{
		"manifest.json": []byte(`{"format": 2}`),
		"goals.json":    []byte(`[{"name": "weekly", "metric": "distance", "target": 100000, "period": "week", "sports": []}]`),
	}, "manifest.json", "goals.json")
	pending := func() models.AccountImport {
		t.Helper()
		if err := os.MkdirAll(importsDir(user.UUID), 0o770); err != nil {
			t.Fatal(err)
		}
		imp := models.AccountImport{ID: uuid.New(), UserID: user.ID, Format: 2}
		imp.ZipPath = importsDir(user.UUID) + "/" + imp.ID.String() + ".zip"
		if err := os.WriteFile(imp.ZipPath, export, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := db.CreateAccountImport(&imp); err != nil {
			t.Fatal(err)
		}
		return imp
	}
	App := &app.App{DB: *db, Logger: zerolog.Nop()}

	// interrupted: running in the database, nothing running in the process
	interrupted := pending()
	if err := db.Model(&models.AccountImport{}).Where("id = ?", interrupted.ID).Update("status", models.ImportRunning).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAccountImport(&models.AccountImport{UserID: user.ID}); !errors.Is(err, models.ErrImportRunning) {
		t.Errorf("a second import at once: %v", err)
	}
	ResumeAccountImports(App)
	if !App.Wait(time.Minute) {
		t.Fatal("the resumed import doesn't end")
	}
	resumed, err := db.GetAccountImport(interrupted.ID)
	if err != nil || resumed.Status != models.ImportDone || resumed.Report.Counts["goals"].Created != 1 {
		t.Fatalf("resumed: %+v %v", resumed, err)
	}
	if _, err := os.Stat(interrupted.ZipPath); !os.IsNotExist(err) {
		t.Errorf("the zip of the resumed import stays: %v", err)
	}

	// cancelled before it runs
	cancelled := pending()
	token, _, err := utils.NewSessionToken(user.ID, user.UUID, user.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.DELETE("/user/import", middlewares.IsAuthorized(), CancelAccountImport)
	cancel := func() int {
		req := httptest.NewRequest(http.MethodDelete, "/user/import", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}
	if code := cancel(); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	if got, _ := db.GetAccountImport(cancelled.ID); got.Status != models.ImportCancelled || got.FinishedAt == nil {
		t.Errorf("cancelled: %+v", got)
	}
	if _, err := os.Stat(cancelled.ZipPath); !os.IsNotExist(err) {
		t.Errorf("the zip of the cancelled import stays: %v", err)
	}
	if code := cancel(); code != http.StatusConflict {
		t.Errorf("nothing to cancel: %d", code)
	}
	// a cancelled import isn't resumed
	ResumeAccountImports(App)
	App.Wait(time.Minute)
	if got, _ := db.GetAccountImport(cancelled.ID); got.Status != models.ImportCancelled {
		t.Errorf("resumed after its cancel: %s", got.Status)
	}
}

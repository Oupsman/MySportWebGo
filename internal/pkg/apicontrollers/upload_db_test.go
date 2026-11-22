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
	"bytes"
	"compress/gzip"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestUploadGPXActivity(t *testing.T) {
	db := testDB(t)
	t.Chdir(t.TempDir())
	saved := activityService.MapTiles
	activityService.MapTiles = staticmap.NewTiles(t.TempDir(), "") // offline: blank tiles
	t.Cleanup(func() { activityService.MapTiles = saved })
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User"}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&alice, alice.ID).Error; err != nil {
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
	router.POST("/activity", middlewares.IsAuthorized(models.ActivitiesCreate), UploadActivity)
	router.DELETE("/activity/:id", middlewares.IsAuthorized(models.ActivitiesDelete), DeleteActivity)

	upload := func(name string, content []byte, sport string) (int, map[string]interface{}) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", name)
		part.Write(content)
		if sport != "" {
			form.WriteField("sport", sport)
		}
		form.Close()
		req := httptest.NewRequest(http.MethodPost, "/activity", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		answer := map[string]interface{}{}
		json.Unmarshal(w.Body.Bytes(), &answer)
		return w.Code, answer
	}
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(converttest.GPX(false))
	w.Close()

	code, answer := upload("Morning_Ride.gpx.gz", gz.Bytes(), "hiking")
	if code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, answer)
	}
	id := uuid.MustParse(answer["id"].(string))
	activity, err := db.GetActivity(id)
	if err != nil {
		t.Fatal(err)
	}
	if activity.Sport != "hiking" || activity.Filename != "Morning_Ride.gpx.gz" ||
		!strings.HasSuffix(activity.FilePath, "Morning_Ride.fit") || !strings.HasSuffix(activity.SourceFilePath, "Morning_Ride.gpx") {
		t.Errorf("activity: %s, %s, %s, %s", activity.Sport, activity.Filename, activity.FilePath, activity.SourceFilePath)
	}
	for _, p := range []string{activity.FilePath, activity.SourceFilePath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if source, _ := os.ReadFile(activity.SourceFilePath); !bytes.Equal(source, converttest.GPX(false)) {
		t.Error("the GPX file isn't kept decompressed")
	}
	// only the user's own files under MEDIA
	if !strings.HasPrefix(activity.SourceFilePath, filepath.Join("MEDIA", alice.UUID.String())) {
		t.Errorf("stored at %s", activity.SourceFilePath)
	}

	// the same activity again: the same sport and start, refused
	if code, answer := upload("again.gpx", converttest.GPX(false), "hiking"); code != http.StatusConflict {
		t.Errorf("duplicate: %d %v", code, answer)
	}
	for name, test := range map[string]struct {
		file, sport string
		content     []byte
	}{
		"unknown sport": {"x.gpx", "biking", converttest.TCX()},
		"not a track":   {"x.gpx", "", []byte("<gpx></gpx>")},
		"not a file":    {"x.fit", "", []byte("hello")},
	} {
		if code, answer := upload(test.file, test.content, test.sport); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, answer)
		}
	}
	// a TCX names its sport: the field doesn't change it
	code, answer = upload("run.tcx", converttest.TCX(), "cycling")
	if code != http.StatusOK {
		t.Fatalf("TCX: %d %v", code, answer)
	}
	if run, _ := db.GetActivity(uuid.MustParse(answer["id"].(string))); run.Sport != "running" {
		t.Errorf("TCX sport %s", run.Sport)
	}

	// deleted: the FIT file and the GPX file go
	req := httptest.NewRequest(http.MethodDelete, "/activity/"+id.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{activity.FilePath, activity.SourceFilePath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left after the delete: %v", p, err)
		}
	}
}

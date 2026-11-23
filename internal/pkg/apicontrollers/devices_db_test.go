package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func TestUpdateDevice(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User"}
	bob := models.Users{Username: "bob", Email: "bob@test.local", Role: "User"}
	for _, u := range []*models.Users{&alice, &bob} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(u, u.ID).Error; err != nil { // the uuid is set by the database
			t.Fatal(err)
		}
	}
	edge := types.Device{Brand: "Garmin", Model: "Edge 530", Serial: "3999999999"}
	hers, err := db.FindOrCreateDevice(alice.ID, edge, "Garmin Edge 530", models.AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	his, err := db.FindOrCreateDevice(bob.ID, edge, "Garmin Edge 530", models.AltitudeCorrectionNone)
	if err != nil {
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
	router.GET("/device/:id", middlewares.IsAuthorized(models.ActivitiesRead), GetDevice)
	router.POST("/device/:id", middlewares.IsAuthorized(models.ActivitiesWrite), UpdateDevice)
	router.POST("/device/:id/merge", middlewares.IsAuthorized(models.ActivitiesWrite), MergeDevice)
	router.POST("/device/:id/recompute", middlewares.IsAuthorized(models.ActivitiesWrite), RecomputeDevice)
	client := sessionClient{t: t, router: router}
	correction := func(id string) string {
		var device models.Devices
		if err := db.First(&device, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		return device.AltitudeCorrection
	}

	code, answer := client.send(http.MethodPost, "/device/"+hers.String(), token, `{"altitude_correction": "replace"}`)
	if code != http.StatusOK || answer["altitude_correction"] != "replace" || answer["serial_number"] != edge.Serial {
		t.Errorf("update: %d %v", code, answer)
	}
	if got := correction(hers.String()); got != models.AltitudeCorrectionReplace {
		t.Errorf("stored correction %q", got)
	}

	// renamed, trimmed, the correction kept; then back to the name of its files
	code, answer = client.send(http.MethodPost, "/device/"+hers.String(), token, `{"label": "  Gravel Edge "}`)
	if code != http.StatusOK || answer["label"] != "Gravel Edge" || answer["name"] != "Garmin Edge 530" ||
		answer["altitude_correction"] != "replace" {
		t.Errorf("rename: %d %v", code, answer)
	}
	code, answer = client.send(http.MethodPost, "/device/"+hers.String(), token, `{"label": ""}`)
	if code != http.StatusOK || answer["label"] != "" {
		t.Errorf("label removed: %d %v", code, answer)
	}

	for _, refused := range []struct {
		name, path, body string
		code             int
	}{
		{"unknown correction", "/device/" + hers.String(), `{"altitude_correction": "dem"}`, http.StatusBadRequest},
		{"nothing to change", "/device/" + hers.String(), `{}`, http.StatusBadRequest},
		{"label too long", "/device/" + hers.String(), `{"label": "` + strings.Repeat("é", models.MaxDeviceLabel+1) + `"}`,
			http.StatusBadRequest},
		{"label of another user's device", "/device/" + his.String(), `{"label": "mine"}`, http.StatusNotFound},
		{"not an id", "/device/edge", `{"altitude_correction": "none"}`, http.StatusBadRequest},
		// the same answer whether the device exists or belongs to another user
		{"device of another user", "/device/" + his.String(), `{"altitude_correction": "replace"}`, http.StatusNotFound},
	} {
		if code, answer := client.send(http.MethodPost, refused.path, token, refused.body); code != refused.code {
			t.Errorf("%s: %d %v, want %d", refused.name, code, answer, refused.code)
		}
	}
	if got := correction(hers.String()); got != models.AltitudeCorrectionReplace {
		t.Errorf("correction %q after the refused updates", got)
	}
	if got := correction(his.String()); got != models.AltitudeCorrectionNone {
		t.Errorf("the other user's device was changed to %q", got)
	}
	if code, _ := client.send(http.MethodGet, "/device/"+his.String(), token, ""); code != http.StatusNotFound {
		t.Errorf("reading the other user's device: %d", code)
	}
	if code, answer := client.send(http.MethodGet, "/device/"+hers.String(), token, ""); code != http.StatusOK ||
		answer["name"] != "Garmin Edge 530" {
		t.Errorf("reading her device: %d %v", code, answer)
	}

	// merges
	older, err := db.FindOrCreateDevice(alice.ID, types.Device{Brand: "Garmin", Model: "Edge 530"}, "Garmin Edge 530",
		models.AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	merge := func(id string, body string) (int, map[string]string) {
		return client.send(http.MethodPost, "/device/"+id+"/merge", token, body)
	}
	for _, refused := range []struct {
		name, id, body string
		code           int
	}{
		{"into itself", older.String(), `{"into": "` + older.String() + `"}`, http.StatusBadRequest},
		{"into nothing", older.String(), `{}`, http.StatusBadRequest},
		{"into not an id", older.String(), `{"into": "edge"}`, http.StatusBadRequest},
		{"into another user's", older.String(), `{"into": "` + his.String() + `"}`, http.StatusNotFound},
		{"another user's", his.String(), `{"into": "` + hers.String() + `"}`, http.StatusNotFound},
	} {
		if code, answer := merge(refused.id, refused.body); code != refused.code {
			t.Errorf("merge %s: %d %v, want %d", refused.name, code, answer, refused.code)
		}
	}
	if code, answer := merge(older.String(), `{"into": "`+hers.String()+`"}`); code != http.StatusOK ||
		answer["id"] != hers.String() {
		t.Errorf("merge: %d %v", code, answer)
	}
	if code, _ := client.send(http.MethodGet, "/device/"+older.String(), token, ""); code != http.StatusNotFound {
		t.Errorf("reading the merged device: %d", code)
	}

	// recomputes: of her device (no activity, done at once), not of another user's nor of a merged one
	recompute := func(id string) int {
		code, _ := client.send(http.MethodPost, "/device/"+id+"/recompute", token, "")
		return code
	}
	if code := recompute(hers.String()); code != http.StatusAccepted {
		t.Errorf("recompute: %d", code)
	}
	for name, id := range map[string]string{"another user's": his.String(), "merged": older.String(), "not an id": "edge"} {
		want := http.StatusNotFound
		if name == "not an id" {
			want = http.StatusBadRequest
		}
		if code := recompute(id); code != want {
			t.Errorf("recompute of the %s device: %d, want %d", name, code, want)
		}
	}
}

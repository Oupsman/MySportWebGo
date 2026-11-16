package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/http"
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

	for _, refused := range []struct {
		name, path, body string
		code             int
	}{
		{"unknown correction", "/device/" + hers.String(), `{"altitude_correction": "dem"}`, http.StatusBadRequest},
		{"no correction", "/device/" + hers.String(), `{}`, http.StatusBadRequest},
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
}

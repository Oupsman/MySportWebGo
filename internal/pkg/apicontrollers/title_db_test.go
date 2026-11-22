package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestSetActivityTitle(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	alice := models.Users{Username: "alice", Email: "alice@test.local", Role: "User"}
	bob := models.Users{Username: "bob", Email: "bob@test.local", Role: "User"}
	for _, u := range []*models.Users{&alice, &bob} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(u, u.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	bike := models.Equipments{Name: "bike", UserID: int(alice.ID)}
	if err := db.CreateEquipment(&bike); err != nil {
		t.Fatal(err)
	}
	hers := models.Activity{ID: uuid.New(), UserID: alice.ID, Title: "Morning ride", Sport: "cycling",
		Date: time.Now(), Visibility: 2, IsCommute: true, EquipmentID: &bike.ID}
	his := models.Activity{ID: uuid.New(), UserID: bob.ID, Title: "His ride", Sport: "cycling", Date: time.Now()}
	for _, a := range []*models.Activity{&hers, &his} {
		if err := db.Omit("User", "Equipment").Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	token, _, err := utils.NewSessionToken(alice.ID, alice.UUID, alice.Role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.POST("/activity/:id/title", middlewares.IsAuthorized(models.ActivitiesWrite), SetActivityTitle)
	client := jsonClient{router: router, token: token}

	code, answer := client.send(http.MethodPost, "/activity/"+hers.ID.String()+"/title", `{"title": "  Col du Galibier "}`)
	if code != http.StatusOK || answer["title"] != "Col du Galibier" {
		t.Fatalf("rename: %d %v", code, answer)
	}
	got, err := db.GetActivity(hers.ID)
	if err != nil {
		t.Fatal(err)
	}
	// only the title changed
	if got.Title != "Col du Galibier" || got.Visibility != 2 || !got.IsCommute || got.EquipmentID == nil ||
		*got.EquipmentID != bike.ID {
		t.Errorf("after rename: title %q, visibility %d, commute %v, equipment %v",
			got.Title, got.Visibility, got.IsCommute, got.EquipmentID)
	}

	for _, refused := range []struct {
		name, path, body string
		code             int
	}{
		{"empty", "/activity/" + hers.ID.String() + "/title", `{"title": "   "}`, http.StatusBadRequest},
		{"too long", "/activity/" + hers.ID.String() + "/title",
			`{"title": "` + strings.Repeat("é", MaxTitleLength+1) + `"}`, http.StatusBadRequest},
		{"not an id", "/activity/ride/title", `{"title": "x"}`, http.StatusBadRequest},
		// the same answer whether the activity exists or belongs to another user
		{"another user's", "/activity/" + his.ID.String() + "/title", `{"title": "mine"}`, http.StatusNotFound},
		{"unknown", "/activity/" + uuid.NewString() + "/title", `{"title": "x"}`, http.StatusNotFound},
	} {
		if code, answer := client.send(http.MethodPost, refused.path, refused.body); code != refused.code {
			t.Errorf("%s: %d %v, want %d", refused.name, code, answer, refused.code)
		}
	}
	if got, _ := db.GetActivity(his.ID); got.Title != "His ride" {
		t.Errorf("another user's activity renamed: %q", got.Title)
	}
	// exactly the limit is accepted
	long := strings.Repeat("é", MaxTitleLength)
	if code, _ := client.send(http.MethodPost, "/activity/"+hers.ID.String()+"/title", `{"title": "`+long+`"}`); code != http.StatusOK {
		t.Errorf("a title of %d characters: %d", MaxTitleLength, code)
	}
}

package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"MySportWeb/services/activityService"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// the thumbnails are served to whom may see their activity, the pictures to their owner only; the others
// get 404, as for a missing file
func TestMediaAccess(t *testing.T) {
	db := testDB(t)
	vars.SecretKey = "testsecret"
	t.Chdir(t.TempDir())
	users := createUsers(t, db, "password", "owner", "other")
	owner, other := users[0], users[1]

	thumbnail := map[int]string{}
	var public uuid.UUID
	for _, visibility := range []int{visibilityPrivate, visibilityUsers, visibilityPublic} {
		activity := models.Activity{ID: uuid.New(), UserID: owner.ID, Visibility: visibility, Title: "ride", Sport: "cycling"}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
		path := activityService.ThumbnailPath(owner.UUID, activity.ID)
		write(t, path, "png")
		thumbnail[visibility] = activityService.ThumbnailURL(owner.UUID, activity.ID)
		if visibility == visibilityPublic {
			public = activity.ID
		}
	}
	picture := "/MEDIA/" + owner.UUID.String() + "/equipments/" + uuid.NewString() + "/" + uuid.NewString() + ".jpg"
	write(t, filepath.FromSlash(picture[1:]), "jpg")
	// a thumbnail of a public activity of the owner, put in the directory of the other user
	misplaced := activityService.ThumbnailURL(other.UUID, public)
	write(t, filepath.FromSlash(misplaced[1:]), "png")

	gin.SetMode(gin.TestMode)
	router := gin.New()
	App := &app.App{DB: *db, Logger: zerolog.Nop()}
	router.Use(func(c *gin.Context) { c.Set("App", App); c.Next() })
	router.GET("/MEDIA/*path", middlewares.OptionalUser(), ServeMedia)

	cookie := func(user models.Users) *http.Cookie {
		token, _, err := utils.NewSessionToken(user.ID, user.UUID, user.Role, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: middlewares.SessionCookieName(), Value: token}
	}
	for _, c := range []struct {
		about  string
		path   string
		cookie *http.Cookie
		want   int
	}{
		{"owner, private thumbnail", thumbnail[visibilityPrivate], cookie(owner), http.StatusOK},
		{"owner, picture", picture, cookie(owner), http.StatusOK},
		{"other user, private thumbnail", thumbnail[visibilityPrivate], cookie(other), http.StatusNotFound},
		{"other user, users' thumbnail", thumbnail[visibilityUsers], cookie(other), http.StatusOK},
		{"other user, public thumbnail", thumbnail[visibilityPublic], cookie(other), http.StatusOK},
		{"other user, picture", picture, cookie(other), http.StatusNotFound},
		{"anonymous, private thumbnail", thumbnail[visibilityPrivate], nil, http.StatusNotFound},
		{"anonymous, users' thumbnail", thumbnail[visibilityUsers], nil, http.StatusNotFound},
		{"anonymous, public thumbnail", thumbnail[visibilityPublic], nil, http.StatusOK},
		{"anonymous, picture", picture, nil, http.StatusNotFound},
		{"anonymous, thumbnail of no activity", activityService.ThumbnailURL(owner.UUID, uuid.New()), nil, http.StatusNotFound},
		{"anonymous, public thumbnail under another user", misplaced, nil, http.StatusNotFound},
		{"ended session: anonymous", thumbnail[visibilityPublic], &http.Cookie{Name: middlewares.SessionCookieName(), Value: "garbage"}, http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.cookie != nil {
			req.AddCookie(c.cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.about, w.Code, c.want)
		}
		if w.Code == http.StatusOK && w.Header().Get("Cache-Control") != "private, no-cache" {
			t.Errorf("%s: Cache-Control %q", c.about, w.Header().Get("Cache-Control"))
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

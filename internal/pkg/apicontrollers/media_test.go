package apicontrollers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServeMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Chdir(t.TempDir())
	const (
		user      = "0b5d3e0e-3c4a-4d9f-9a63-0f0d7c1b2a11"
		activity  = "6f1c2b8e-1d2a-4b3c-8e9f-0a1b2c3d4e5f"
		equipment = "9a8b7c6d-5e4f-4a3b-9c2d-1e0f9a8b7c6d"
		picture   = "11111111-2222-4333-8444-555555555555"
	)
	files := map[string]string{
		"thumbnails/" + activity + ".png":                  "png",
		"equipments/" + equipment + "/" + picture + ".jpg": "jpg",
		"Activities/ride.fit":                              "fit",
		"Activities/" + activity + ".png":                  "fit renamed",
	}
	for name, content := range files {
		path := filepath.Join("MEDIA", user, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	router.GET("/MEDIA/*path", ServeMedia)

	for _, tt := range []struct {
		path string
		want int
	}{
		{"/MEDIA/" + user + "/thumbnails/" + activity + ".png", http.StatusOK},
		{"/MEDIA/" + user + "/equipments/" + equipment + "/" + picture + ".jpg", http.StatusOK},
		{"/MEDIA/" + user + "/thumbnails/" + equipment + ".png", http.StatusNotFound}, // no such file
		{"/MEDIA/", http.StatusNotFound},
		{"/MEDIA/" + user + "/", http.StatusNotFound},
		{"/MEDIA/" + user + "/thumbnails/", http.StatusNotFound},
		{"/MEDIA/" + user + "/Activities/", http.StatusNotFound},
		{"/MEDIA/" + user + "/Activities/ride.fit", http.StatusNotFound},
		{"/MEDIA/" + user + "/Activities/" + activity + ".png", http.StatusNotFound},
		{"/MEDIA/" + user + "/thumbnails/../Activities/ride.fit", http.StatusNotFound},
		{"/MEDIA/" + user + "/thumbnails/%2e%2e%2fActivities%2fride.fit", http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
		// gin may redirect a path to clean it, never to a served file of another directory
		if w.Code == http.StatusMovedPermanently || w.Code == http.StatusTemporaryRedirect {
			continue
		}
		if w.Code != tt.want {
			t.Errorf("%s: %d, want %d", tt.path, w.Code, tt.want)
		}
		if w.Code == http.StatusOK && w.Body.String() == "fit" {
			t.Errorf("%s served a FIT file", tt.path)
		}
	}
}

package apicontrollers

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/services/activityService"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// answer runs answer in a request, it returns the status, the body and what was logged
func answer(t *testing.T, respond func(c *gin.Context)) (int, string, string) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("App", &app.App{Logger: zerolog.New(&logs)}); c.Next() })
	router.GET("/x", respond)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	return w.Code, w.Body.String(), logs.String()
}

func TestErrorAnswers(t *testing.T) {
	dbError := errors.New(`pq: relation "activities" does not exist, host 10.0.0.3`)
	pathError := &fs.PathError{Op: "open", Path: "MEDIA/0b5d3e0e/Activities/ride.fit", Err: fs.ErrNotExist}
	notFit := filepath.Join(t.TempDir(), "notes.fit")
	if err := os.WriteFile(notFit, []byte("these are not the records of a ride"), 0600); err != nil {
		t.Fatal(err)
	}
	_, invalidFit := activityService.AnalyzeTrimmed(notFit, models.Users{}, models.Equipments{}, 0, activityService.Analysis{})
	var invalid *activityService.InvalidFitError
	if !errors.As(invalidFit, &invalid) {
		t.Fatalf("a text file analysed: %v, want an InvalidFitError", invalidFit)
	}
	for _, tt := range []struct {
		name     string
		respond  func(c *gin.Context)
		want     int
		wantBody string // in the answer
		secret   string // never in the answer, always in the logs
	}{
		{"database error", func(c *gin.Context) { internalError(c, dbError) }, 500, "internal error", "10.0.0.3"},
		{"lookup of a missing record", func(c *gin.Context) { lookupError(c, gorm.ErrRecordNotFound, "activity") },
			404, "activity not found", ""},
		{"lookup failing", func(c *gin.Context) { lookupError(c, dbError, "activity") }, 500, "internal error", "10.0.0.3"},
		{"invalid FIT file", func(c *gin.Context) { analysisError(c, fmt.Errorf("upload: %w", invalidFit)) },
			400, "not a FIT file", ""},
		{"trim too short", func(c *gin.Context) { analysisError(c, activityService.ErrTrimTooShort) },
			400, activityService.ErrTrimTooShort.Error(), ""},
		{"FIT file missing", func(c *gin.Context) { analysisError(c, activityService.ErrNoFitFile) },
			400, activityService.ErrNoFitFile.Error(), ""},
		{"busy", func(c *gin.Context) { analysisError(c, activityService.ErrBusy) }, 503, "busy", ""},
		{"FIT file unreadable", func(c *gin.Context) { analysisError(c, pathError) }, 500, "internal error", "MEDIA/0b5d3e0e"},
		// the resting heart rate of the training score is read from the database during the analysis
		{"database error during the analysis", func(c *gin.Context) { analysisError(c, dbError) },
			500, "internal error", "10.0.0.3"},
	} {
		code, body, logs := answer(t, tt.respond)
		if code != tt.want {
			t.Errorf("%s: %d %s, want %d", tt.name, code, body, tt.want)
		}
		if !strings.Contains(body, tt.wantBody) {
			t.Errorf("%s: %s, want %q in it", tt.name, body, tt.wantBody)
		}
		if tt.secret != "" && (strings.Contains(body, tt.secret) || !strings.Contains(logs, tt.secret)) {
			t.Errorf("%s: answered %s, logged %s", tt.name, body, logs)
		}
	}
}

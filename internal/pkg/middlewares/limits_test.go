package middlewares

import (
	"MySportWeb/internal/pkg/types"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func limitedRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LimitBody(1<<20, map[string]int64{"/upload/:id": 2 << 20}))
	json := func(c *gin.Context) {
		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err != nil {
			if !RequestTooLarge(c, err) {
				c.AbortWithStatus(http.StatusBadRequest)
			}
			return
		}
		c.Status(http.StatusOK)
	}
	// as UploadActivity binds the FIT files
	upload := func(c *gin.Context) {
		var params types.ActivityUpload
		if err := c.ShouldBind(&params); err != nil {
			if !RequestTooLarge(c, err) {
				c.AbortWithStatus(http.StatusBadRequest)
			}
			return
		}
		c.Status(http.StatusOK)
	}
	router.POST("/json", json)
	router.POST("/upload/:id", upload)
	return router
}

func jsonBody(size int) string {
	return `{"padding": "` + strings.Repeat("x", size) + `"}`
}

func multipartBody(t *testing.T, size int) (io.Reader, string) {
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	part, err := form.CreateFormFile("file", "ride.fit")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(bytes.Repeat([]byte{1}, size))
	form.Close()
	return &b, form.FormDataContentType()
}

// hideLength makes a body of unknown length, as a chunked request: only the reader stops it
type hideLength struct{ io.Reader }

func TestLimitBody(t *testing.T) {
	router := limitedRouter()
	for _, tt := range []struct {
		name        string
		path        string
		body        func() (io.Reader, string)
		chunked     bool
		want        int
		wantMessage string
	}{
		{"small JSON", "/json", func() (io.Reader, string) { return strings.NewReader(jsonBody(1000)), "application/json" }, false, http.StatusOK, ""},
		{"JSON over the default", "/json", func() (io.Reader, string) { return strings.NewReader(jsonBody(2 << 20)), "application/json" }, false, http.StatusRequestEntityTooLarge, "1 MB"},
		{"chunked JSON over the default", "/json", func() (io.Reader, string) { return strings.NewReader(jsonBody(2 << 20)), "application/json" }, true, http.StatusRequestEntityTooLarge, "1 MB"},
		{"file under its route's limit", "/upload/1", func() (io.Reader, string) { return multipartBody(t, 3<<19) }, false, http.StatusOK, ""},
		{"file over its route's limit", "/upload/1", func() (io.Reader, string) { return multipartBody(t, 3<<20) }, false, http.StatusRequestEntityTooLarge, "2 MB"},
		{"chunked file over its route's limit", "/upload/1", func() (io.Reader, string) { return multipartBody(t, 3<<20) }, true, http.StatusRequestEntityTooLarge, "2 MB"},
	} {
		body, contentType := tt.body()
		if tt.chunked {
			body = hideLength{body}
		}
		req := httptest.NewRequest(http.MethodPost, tt.path, body)
		req.Header.Set("Content-Type", contentType)
		if tt.chunked && req.ContentLength > 0 {
			t.Fatalf("%s: the request announces %d bytes", tt.name, req.ContentLength)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tt.want {
			t.Errorf("%s: %d, want %d (%s)", tt.name, w.Code, tt.want, w.Body.String())
		}
		if tt.wantMessage != "" && !strings.Contains(w.Body.String(), tt.wantMessage) {
			t.Errorf("%s: %s, want the limit %s", tt.name, w.Body.String(), tt.wantMessage)
		}
	}
}

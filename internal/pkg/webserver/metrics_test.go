package webserver

import (
	"MySportWeb/internal/pkg/metrics"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// the requests are counted by route pattern, never by path: one series per route, not per activity
func TestInstrument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(instrument)
	router.GET("/api/v1/activity/:id", func(c *gin.Context) { c.Status(http.StatusTeapot) })

	count := func(route, status string) float64 {
		return testutil.ToFloat64(metrics.HTTPRequests.WithLabelValues(http.MethodGet, route, status))
	}
	before, unmatched := count("/api/v1/activity/:id", "418"), count("unmatched", "404")
	for _, path := range []string{"/api/v1/activity/1", "/api/v1/activity/2", "/nowhere"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if got := count("/api/v1/activity/:id", "418") - before; got != 2 {
		t.Errorf("by route: %v, want 2", got)
	}
	if got := count("unmatched", "404") - unmatched; got != 1 {
		t.Errorf("unmatched: %v, want 1", got)
	}
	if n := testutil.CollectAndCount(metrics.HTTPDuration, "mysportweb_http_request_duration_seconds"); n < 2 {
		t.Errorf("%d duration series", n)
	}
}

func TestMetricsToken(t *testing.T) {
	scrape := func(handler http.Handler, authorization string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	open := scrape(metricsHandler(""), "")
	if open.Code != http.StatusOK || !strings.Contains(open.Body.String(), "go_goroutines") {
		t.Fatalf("without token: %d", open.Code)
	}
	protected := metricsHandler("s3cret")
	for _, authorization := range []string{"", "Bearer wrong", "s3cret", "Basic s3cret"} {
		if w := scrape(protected, authorization); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "go_goroutines") {
			t.Errorf("Authorization %q: %d", authorization, w.Code)
		}
	}
	if w := scrape(protected, "Bearer s3cret"); w.Code != http.StatusOK {
		t.Errorf("right token: %d", w.Code)
	}
	if w := httptest.NewRecorder(); true {
		protected.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("another path: %d", w.Code)
		}
	}
}

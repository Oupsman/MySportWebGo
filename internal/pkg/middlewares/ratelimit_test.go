package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	l := NewRateLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait != time.Minute {
		t.Fatalf("4th attempt: %v, wait %v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("another key is limited too")
	}
	// a refused attempt doesn't extend the window
	now = now.Add(59 * time.Second)
	if blocked, wait := l.Blocked("a"); !blocked || wait != time.Second {
		t.Fatalf("near the end of the window: %v, wait %v", blocked, wait)
	}
	now = now.Add(time.Second)
	if blocked, _ := l.Blocked("a"); blocked {
		t.Fatal("still blocked after the window")
	}

	// Add counts without being refused, Reset forgets
	for i := 0; i < 5; i++ {
		l.Add("c")
	}
	if blocked, _ := l.Blocked("c"); !blocked {
		t.Fatal("5 events of 3 not blocked")
	}
	l.Reset("c")
	if blocked, _ := l.Blocked("c"); blocked {
		t.Fatal("blocked after Reset")
	}

	// the ended windows are forgotten
	now = now.Add(2 * time.Minute)
	l.Add("d")
	if len(l.counts) != 1 {
		t.Errorf("%d keys kept, want only d", len(l.counts))
	}
}

func TestRateLimitPerClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := router.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	router.POST("/login", RateLimit(NewRateLimiter(2, time.Minute)), func(c *gin.Context) { c.Status(http.StatusOK) })
	send := func(remote, forwarded string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = remote + ":12345"
		if forwarded != "" {
			req.Header.Set("X-Forwarded-For", forwarded)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// behind the trusted proxy, each client has its own limit
	for _, client := range []string{"203.0.113.1", "203.0.113.2"} {
		for i := 0; i < 2; i++ {
			if w := send("10.0.0.5", client); w.Code != http.StatusOK {
				t.Fatalf("%s attempt %d: %d", client, i+1, w.Code)
			}
		}
	}
	w := send("10.0.0.5", "203.0.113.1")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("3rd attempt: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}

	// a client reaching the server directly can't choose its IP with the header
	for i := 0; i < 2; i++ {
		send("198.51.100.9", "192.0.2.100")
	}
	if w := send("198.51.100.9", "192.0.2.200"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Forwarded-For escaped the limit: %d", w.Code)
	}
}

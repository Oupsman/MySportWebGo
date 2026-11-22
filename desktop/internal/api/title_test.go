package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestUploadBusy(t *testing.T) {
	for header, want := range map[string]time.Duration{"12": 12 * time.Second, "": defaultRetryAfter, "soon": defaultRetryAfter} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if header != "" {
				w.Header().Set("Retry-After", header)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"busy"}`))
		}))
		client, _ := New(server.URL, "msw_x")
		_, err := client.Upload(context.Background(), "a.fit", []byte("x"), 0, 1, "")
		var busy *BusyError
		if !errors.As(err, &busy) || busy.RetryAfter != want {
			t.Errorf("Retry-After %q: %v, want a wait of %s", header, err, want)
		}
		server.Close()
	}
}

func TestSetTitle(t *testing.T) {
	var got struct{ Title string }
	var path string
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		w.Write([]byte(`{"message":"lacks activities:write"}`))
	}))
	defer server.Close()
	client, _ := New(server.URL, "msw_x")
	long := strings.Repeat("é", MaxTitleLength+10)
	if err := client.SetTitle(context.Background(), "abc", long); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/activity/abc/title" || utf8.RuneCountInString(got.Title) != MaxTitleLength {
		t.Errorf("path %s, title of %d characters", path, utf8.RuneCountInString(got.Title))
	}
	status = http.StatusForbidden
	if err := client.SetTitle(context.Background(), "abc", "x"); !errors.Is(err, ErrAuth) {
		t.Errorf("refused: %v", err)
	}
}

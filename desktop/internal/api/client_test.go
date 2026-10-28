package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"MySportWebSync/internal/health"
)

const token = "msw_test"

// backend answers as MySportWeb does, by the name of the uploaded file
func backend(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/activity" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"message":"unauthorized: invalid device token"}`)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("no file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		content, _ := io.ReadAll(file)
		if string(content) != "FIT" || r.FormValue("item") != "2" || r.FormValue("count") != "5" {
			t.Errorf("content %q, item %q, count %q", content, r.FormValue("item"), r.FormValue("count"))
		}
		switch header.Filename {
		case "new.fit":
			io.WriteString(w, `{"id":"0b6c"}`)
		case "dup.fit":
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"error":"this activity was already imported","id":"77aa"}`)
		case "bad.fit":
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"not a FIT file"}`)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
}

func TestUpload(t *testing.T) {
	server := backend(t)
	defer server.Close()
	client, err := New(server.URL+"/", token)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	tests := []struct {
		name   string
		status Status
		id     string
	}{
		{"new.fit", Uploaded, "0b6c"},
		{"dup.fit", Duplicate, "77aa"},
		{"bad.fit", Rejected, ""},
	}
	for _, tt := range tests {
		result, err := client.Upload(ctx, tt.name, []byte("FIT"), 2, 5)
		if err != nil || result.Status != tt.status || result.ActivityID != tt.id {
			t.Errorf("%s: %+v, %v", tt.name, result, err)
		}
	}
	if _, err := client.Upload(ctx, "proxy.fit", []byte("FIT"), 2, 5); err == nil || errors.Is(err, ErrAuth) {
		t.Errorf("a failing proxy: %v, want an error to try again", err)
	}

	other, _ := New(server.URL, "msw_revoked")
	if _, err := other.Upload(ctx, "new.fit", []byte("FIT"), 2, 5); !errors.Is(err, ErrAuth) {
		t.Errorf("a revoked token: %v, want ErrAuth", err)
	}
}

func TestNewRefuses(t *testing.T) {
	for _, tt := range []struct{ url, token string }{
		{"mysportweb.example.com", token}, // no scheme
		{"ftp://mysportweb.example.com", token},
		{"https://mysportweb.example.com", "eyJhbGciOi"}, // a session token
	} {
		if _, err := New(tt.url, tt.token); err == nil {
			t.Errorf("%q %q accepted", tt.url, tt.token)
		}
	}
}

func TestCheckToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer msw_uploader":
			io.WriteString(w, `{"device_token":{"id":"1","name":"laptop","hint":"msw_abcd",`+
				`"permissions":["activities:read","activities:create"],"expires_at":"2027-10-28T00:00:00Z"}}`)
		case "Bearer msw_reader":
			io.WriteString(w, `{"device_token":{"name":"reader","permissions":["activities:read"]}}`)
		case "Bearer msw_old_server":
			http.NotFound(w, r)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"message":"unauthorized: expired device token"}`)
		}
	}))
	defer server.Close()
	check := func(token string) (TokenInfo, error) {
		client, err := New(server.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		return client.CheckToken(context.Background())
	}

	info, err := check("msw_uploader")
	if err != nil || info.Name != "laptop" || !info.CanUpload() || info.ExpiresAt.Year() != 2027 {
		t.Errorf("uploader: %+v, %v", info, err)
	}
	if info, err := check("msw_reader"); err != nil || info.CanUpload() {
		t.Errorf("reader: %+v, %v", info, err)
	}
	if _, err := check("msw_old_server"); !errors.Is(err, ErrNoTokenRoute) {
		t.Errorf("old server: %v", err)
	}
	if _, err := check("msw_expired"); !errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired: %v", err)
	}
}

func TestImportHealth(t *testing.T) {
	var requests []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/healthdatas/import" || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer msw_old":
			w.WriteHeader(http.StatusNotImplemented)
			io.WriteString(w, `{"error":"health data import is not implemented yet"}`)
			return
		case "Bearer msw_reader":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"forbidden: the device token lacks the permissions healthdatas:create"}`)
			return
		}
		var body struct {
			Measures []health.Measure `json:"measures"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, len(body.Measures))
		// the second measure of each batch is refused, the others created
		io.WriteString(w, fmt.Sprintf(`{"created":%d,"merged":0,"unchanged":0,"rejected":[{"index":1,"error":"no value measured"}]}`,
			len(body.Measures)-1))
	}))
	defer server.Close()

	measures := make([]health.Measure, 2500)
	for i := range measures {
		measures[i] = health.Measure{Date: time.Unix(int64(i)*86400, 0), Weight: 70}
	}
	client, _ := New(server.URL, "msw_writer")
	var last int
	result, err := client.ImportHealth(context.Background(), measures, func(done, total int) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(requests) != "[1000 1000 500]" || last != 2500 {
		t.Errorf("batches %v, progress %d", requests, last)
	}
	if result.Created != 2497 || len(result.Rejected) != 3 || result.Rejected[1].Index != 1001 || result.Rejected[2].Index != 2001 {
		t.Errorf("%+v", result)
	}

	old, _ := New(server.URL, "msw_old")
	if _, err := old.ImportHealth(context.Background(), measures[:1], nil); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Errorf("old server: %v", err)
	}
	reader, _ := New(server.URL, "msw_reader")
	if _, err := reader.ImportHealth(context.Background(), measures[:1], nil); !errors.Is(err, ErrAuth) {
		t.Errorf("token without healthdatas:create: %v", err)
	}
}

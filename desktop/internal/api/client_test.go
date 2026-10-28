package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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

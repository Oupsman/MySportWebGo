package syncer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"MySportWebSync/internal/api"
	"MySportWebSync/internal/config"
	"MySportWebSync/internal/device"
	"MySportWebSync/internal/state"
)

type fakeDevice struct {
	id       string
	contents map[string]string // by path
	files    []device.File
	reads    int
}

func newFakeDevice(id string, contents map[string]string) *fakeDevice {
	d := &fakeDevice{id: id, contents: contents}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for p, c := range contents {
		d.files = append(d.files, device.File{Path: p, Size: int64(len(c)), ModTime: at})
	}
	return d
}

func (d *fakeDevice) ID() string                                   { return d.id }
func (d *fakeDevice) Name() string                                 { return d.id }
func (d *fakeDevice) Kind() device.Kind                            { return device.MassStorage }
func (d *fakeDevice) Close()                                       {}
func (d *fakeDevice) Files(context.Context) ([]device.File, error) { return d.files, nil }
func (d *fakeDevice) Copy(_ context.Context, f device.File, w io.Writer) error {
	d.reads++
	_, err := io.WriteString(w, d.contents[f.Path])
	return err
}

// fakeServer refuses the files holding "bad", answers 409 for a content already received
type fakeServer struct {
	mu       sync.Mutex
	received map[string]bool
	uploads  int
	token    string
}

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"forbidden: the device token lacks the permissions activities:create"}`)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	content, _ := io.ReadAll(file)
	s.uploads++
	switch {
	case strings.Contains(string(content), "bad"):
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid FIT"}`)
	case s.received[string(content)]:
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"error":"this activity was already imported","id":"x"}`)
	default:
		s.received[string(content)] = true
		io.WriteString(w, `{"id":"new"}`)
	}
}

func setup(t *testing.T) (*Manager, *fakeServer, config.Config) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeServer{received: map[string]bool{"old": true}, token: "msw_good"}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	cfg := config.Default()
	cfg.ServerURL, cfg.Token = server.URL, "msw_good"
	m := NewManager(cfg, st, device.NewMTPScanner(), func() {}, func(string) {})
	return m, fake, cfg
}

func TestUploadOnlyNewFiles(t *testing.T) {
	m, server, cfg := setup(t)
	ctx := context.Background()
	d := newFakeDevice("watch", map[string]string{"A/1.fit": "one", "A/2.fit": "old", "A/3.fit": "bad"})

	c, err := m.upload(ctx, cfg, d, false)
	if err != nil {
		t.Fatal(err)
	}
	if c != (counts{uploaded: 1, duplicates: 1, rejected: 1}) {
		t.Errorf("first sync: %+v", c)
	}

	// the files are known: nothing is read from the device, nor sent
	d.reads, server.uploads = 0, 0
	c, err = m.upload(ctx, cfg, d, false)
	if err != nil || c.handled() != 0 || d.reads != 0 || server.uploads != 0 {
		t.Errorf("second automatic sync: %+v, %v, %d reads, %d uploads", c, err, d.reads, server.uploads)
	}

	// asked by the user: the refused file is tried again, only it
	c, err = m.upload(ctx, cfg, d, true)
	if err != nil || c != (counts{rejected: 1}) || d.reads != 1 {
		t.Errorf("manual sync: %+v, %v, %d reads", c, err, d.reads)
	}

	// the same content on another device, or the same device in another mode: read, not sent
	other := newFakeDevice("watch-mtp", map[string]string{"Internal Storage/A/1.fit": "one"})
	server.uploads = 0
	c, err = m.upload(ctx, cfg, other, false)
	if err != nil || server.uploads != 0 || other.reads != 1 {
		t.Errorf("same content elsewhere: %+v, %v, %d uploads", c, err, server.uploads)
	}
}

func TestUploadStopsOnRefusedToken(t *testing.T) {
	m, server, cfg := setup(t)
	server.token = "msw_other"
	d := newFakeDevice("watch", map[string]string{"A/1.fit": "one", "A/2.fit": "two"})

	_, err := m.upload(context.Background(), cfg, d, false)
	if !errors.Is(err, api.ErrAuth) {
		t.Fatalf("got %v, want ErrAuth", err)
	}
	if server.uploads != 0 {
		t.Errorf("%d uploads", server.uploads)
	}
	// nothing is recorded: the files are sent once the token is fixed
	server.token = "msw_good"
	c, err := m.upload(context.Background(), cfg, d, false)
	if err != nil || c.uploaded != 2 {
		t.Errorf("after fixing the token: %+v, %v", c, err)
	}
}

func TestUploadIgnoresOldFiles(t *testing.T) {
	m, server, cfg := setup(t)
	cfg.MaxAgeDays = 7
	d := newFakeDevice("watch", map[string]string{"A/1.fit": "one"}) // dated 2026-10-01
	d.files = append(d.files, device.File{Path: "A/now.fit", Size: 3, ModTime: time.Now()})
	d.contents["A/now.fit"] = "now"

	c, err := m.upload(context.Background(), cfg, d, false)
	if err != nil || c.uploaded != 1 || server.uploads != 1 {
		t.Errorf("%+v, %v, %d uploads: want only the recent file", c, err, server.uploads)
	}
}

type fakeScanner struct {
	mu      sync.Mutex
	devices []device.Device
}

func (s *fakeScanner) Scan(context.Context, []string) ([]device.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.devices, nil
}

func (s *fakeScanner) set(devices ...device.Device) {
	s.mu.Lock()
	s.devices = devices
	s.mu.Unlock()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManagerSyncsPluggedDevices(t *testing.T) {
	m, server, _ := setup(t)
	scanner := &fakeScanner{}
	m.scanners = []device.Scanner{scanner}
	done := make(chan string, 4)
	m.onDone = func(summary string) { done <- summary }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.scan(ctx)
	go m.worker(ctx)
	if len(m.Devices()) != 0 {
		t.Fatal("a device without plug")
	}

	scanner.set(newFakeDevice("watch", map[string]string{"A/1.fit": "one", "A/2.fit": "two"}))
	m.scan(ctx)
	if summary := <-done; !strings.HasPrefix(summary, "watch: 2 uploaded") {
		t.Errorf("summary %q", summary)
	}
	waitFor(t, "the status", func() bool { d := m.Devices(); return len(d) == 1 && !d[0].Syncing })

	// unplugged then plugged again: synchronized again, nothing to send
	scanner.set()
	m.scan(ctx)
	if len(m.Devices()) != 0 {
		t.Error("the unplugged device is still listed")
	}
	scanner.set(newFakeDevice("watch", map[string]string{"A/1.fit": "one", "A/2.fit": "two"}))
	m.scan(ctx)
	waitFor(t, "the second synchronization", func() bool {
		d := m.Devices()
		return len(d) == 1 && d[0].Last == "nothing new"
	})
	if server.uploads != 2 {
		t.Errorf("%d uploads, want 2", server.uploads)
	}
}

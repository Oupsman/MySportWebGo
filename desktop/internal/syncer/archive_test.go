package syncer

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"MySportWebSync/internal/device"
)

// writeStravaExport writes an export of Strava: activities.csv, two FIT files (one gzipped), a GPX
func writeStravaExport(t *testing.T) string {
	t.Helper()
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte("ride"))
	w.Close()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, content := range map[string][]byte{
		"activities.csv": []byte("Activity ID,Activity Name,Filename\n" +
			"1,Col du Galibier,activities/1.fit.gz\n2,Recovery jog,activities/2.fit\n3,Walk,activities/3.gpx\n"),
		"activities/1.fit.gz": gz.Bytes(),
		"activities/2.fit":    []byte("run"),
		"activities/3.gpx":    []byte("<gpx/>"),
	} {
		f, _ := z.Create(name)
		f.Write(content)
	}
	z.Close()
	path := filepath.Join(t.TempDir(), "export_1.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func noBusyWait(t *testing.T) *[]time.Duration {
	waits := &[]time.Duration{}
	saved := busyWait
	busyWait = func(ctx context.Context, d time.Duration) bool {
		*waits = append(*waits, d)
		return ctx.Err() == nil
	}
	t.Cleanup(func() { busyWait = saved })
	return waits
}

func TestArchiveTitlesAndBusy(t *testing.T) {
	m, server, cfg := setup(t)
	server.canTitle = true
	server.busy = 2
	waits := noBusyWait(t)
	cfg.MaxAgeDays = 1 // the archive's files are old: imported anyway
	a, err := device.OpenArchive(writeStravaExport(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	c, err := m.upload(context.Background(), cfg, a, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.uploaded != 2 || c.titled != 2 || c.skipped != 1 || c.rejected != 0 {
		t.Errorf("counts %+v", c)
	}
	// sent decompressed, without .gz
	if server.titles["1.fit"] != "Col du Galibier" || server.titles["2.fit"] != "Recovery jog" {
		t.Errorf("titles %v", server.titles)
	}
	// the server's Retry-After, twice
	if len(*waits) != 2 || (*waits)[0] != 7*time.Second {
		t.Errorf("waits %v", *waits)
	}
	if !strings.Contains(c.String(), "2 titles set") || !strings.Contains(c.String(), "1 GPX/TCX files skipped") {
		t.Errorf("summary %q", c.String())
	}

	// imported again: nothing is sent, nothing renamed
	server.uploads, server.titles = 0, nil
	c, err = m.upload(context.Background(), cfg, a, false)
	if err != nil || c.handled() != 0 || server.uploads != 0 || server.titles != nil {
		t.Errorf("second import: %+v %v, %d uploads, titles %v", c, err, server.uploads, server.titles)
	}
}

func TestArchiveWithoutTitlePermission(t *testing.T) {
	m, server, cfg := setup(t)
	a, err := device.OpenArchive(writeStravaExport(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	c, err := m.upload(context.Background(), cfg, a, false)
	if err != nil {
		t.Fatal(err)
	}
	// the activities are imported, the titles stop at the first refusal
	if c.uploaded != 2 || c.titled != 0 || !strings.Contains(c.String(), "activities:write") {
		t.Errorf("counts %+v: %s", c, c.String())
	}
	if len(server.titles) != 0 {
		t.Errorf("titles %v", server.titles)
	}
}

func TestBusyServerGivesUp(t *testing.T) {
	m, server, cfg := setup(t)
	server.busy = maxBusyRetries + 1
	noBusyWait(t)
	d := newFakeDevice("watch", map[string]string{"A/1.fit": "one"})
	_, err := m.upload(context.Background(), cfg, d, false)
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("error %v", err)
	}
	if server.uploads != maxBusyRetries+1 {
		t.Errorf("%d uploads", server.uploads)
	}
}

// waitEvent waits for an event of the history holding text
func waitEvent(t *testing.T, m *Manager, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range m.Events() {
			if strings.Contains(e.Text, text) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no event %q in %+v", text, m.Events())
}

func TestImportArchiveCancel(t *testing.T) {
	m, server, _ := setup(t)
	server.busy = 1000
	waiting := make(chan struct{}, 1)
	saved := busyWait
	busyWait = func(ctx context.Context, d time.Duration) bool {
		select {
		case waiting <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return false
	}
	t.Cleanup(func() { busyWait = saved })
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go m.worker(ctx)

	path := writeStravaExport(t)
	if err := m.ImportArchive(path); err != nil {
		t.Fatal(err)
	}
	if err := m.ImportArchive(path); err != ErrImporting {
		t.Errorf("imported twice at once: %v", err)
	}
	<-waiting
	devices := m.Devices()
	if len(devices) != 1 || devices[0].Kind != device.Archive || !devices[0].Syncing {
		t.Errorf("devices %+v", devices)
	}
	m.Cancel(devices[0].ID)
	waitEvent(t, m, "cancelled")
	for deadline := time.Now().Add(5 * time.Second); len(m.Devices()) > 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if len(m.Devices()) != 0 {
		t.Errorf("the cancelled import is still listed: %+v", m.Devices())
	}

	// imported again once the server has time: everything is sent
	server.busy = 0
	busyWait = saved
	if err := m.ImportArchive(path); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, m, "imported: 2 uploaded")
}

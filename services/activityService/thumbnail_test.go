package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/google/uuid"
)

func checkThumbnail(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if b := img.Bounds(); b.Dx() != ThumbnailWidth || b.Dy() != ThumbnailHeight {
		t.Errorf("%s: %dx%d", path, b.Dx(), b.Dy())
	}
}

// without public trace, a blank thumbnail: no map around the hidden trace, and no Chromium to start
func TestBlankThumbnail(t *testing.T) {
	t.Chdir(t.TempDir())
	user := models.Users{UUID: uuid.New()}
	for _, points := range []types.GpsPointArray{nil, {{Lat: 45.1, Lon: 5.7}}} {
		activity := models.Activity{ID: uuid.New(), User: user, PublicGpsPoints: points}
		if err := GenerateThumbnail(activity); err != nil {
			t.Fatalf("%d points: %v", len(points), err)
		}
		checkThumbnail(t, ThumbnailPath(user.UUID, activity.ID))
	}
}

func TestThumbnailPage(t *testing.T) {
	page := thumbnailPage([]types.GpsPoint{{Lat: 45.188529, Lon: 5.724524}, {Lat: 45.19, Lon: 5.73}})
	if !strings.Contains(page, "[[45.188529,5.724524],[45.190000,5.730000]]") {
		t.Errorf("trace not in the page:\n%s", page)
	}
	// the handler is set before the view: setting it starts loading the tiles
	handler, view := strings.Index(page, "tiles.on('load'"), strings.Index(page, "map.fitBounds")
	if handler < 0 || view < 0 || handler > view {
		t.Errorf("the load handler must come before fitBounds:\n%s", page)
	}
	if !strings.Contains(page, "console.log('"+tilesLoaded+"')") {
		t.Errorf("the page doesn't log %q", tilesLoaded)
	}
}

func consoleLog(message string) *runtime.EventConsoleAPICalled {
	return &runtime.EventConsoleAPICalled{Args: []*runtime.RemoteObject{{Value: []byte(`"` + message + `"`)}}}
}

// Leaflet logs the message after each batch of tiles: the listener must never block, chromedp calls it on
// the event loop of the page
func TestTilesListenerNeverBlocks(t *testing.T) {
	loaded := make(chan struct{}, 1)
	listen := tilesListener(loaded)
	done := make(chan struct{})
	go func() {
		listen(consoleLog("something else"))
		listen(&runtime.EventExceptionThrown{})
		for i := 0; i < 5; i++ {
			listen(consoleLog(tilesLoaded))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the listener blocked")
	}
	if len(loaded) != 1 {
		t.Errorf("%d signals, want 1", len(loaded))
	}
}

func TestWaitTiles(t *testing.T) {
	loaded := make(chan struct{}, 1)
	loaded <- struct{}{}
	start := time.Now()
	if err := waitTiles(loaded, time.Minute).Do(context.Background()); err != nil || time.Since(start) > time.Second {
		t.Errorf("tiles loaded: %v after %v", err, time.Since(start))
	}
	// the tiles never come: drawn without them
	if err := waitTiles(make(chan struct{}), 10*time.Millisecond).Do(context.Background()); err != nil {
		t.Errorf("tiles too slow: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitTiles(make(chan struct{}), time.Minute).Do(ctx); err == nil {
		t.Error("the drawing stopped, the wait goes on")
	}
}

// draws a real thumbnail where Chromium is installed (the Docker image); the map needs the network
func TestThumbnailWithChromium(t *testing.T) {
	found := false
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if _, err := exec.LookPath(name); err == nil {
			found = true
		}
	}
	if !found || testing.Short() {
		t.Skip("Chromium not installed")
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("tmp", 0700); err != nil {
		t.Fatal(err)
	}
	user := models.Users{UUID: uuid.New()}
	activity := models.Activity{ID: uuid.New(), User: user,
		PublicGpsPoints: types.GpsPointArray{{Lat: 45.18, Lon: 5.72}, {Lat: 45.19, Lon: 5.73}, {Lat: 45.2, Lon: 5.71}}}
	start := time.Now()
	if err := GenerateThumbnail(activity); err != nil {
		t.Fatal(err)
	}
	checkThumbnail(t, ThumbnailPath(user.UUID, activity.ID))
	t.Logf("drawn in %v", time.Since(start))
	if entries, _ := os.ReadDir("tmp"); len(entries) != 0 {
		t.Errorf("%d pages left in tmp/", len(entries))
	}
}

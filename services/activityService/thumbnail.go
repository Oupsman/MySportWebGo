package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// chromeContext returns a context running a headless Chromium. With CHROMIUM_NO_SANDBOX=true (the
// Docker image sets it), Chromium runs without its own sandbox: Docker's default seccomp profile forbids
// the namespaces it needs, the container isolates it instead. /dev/shm is small in a container too.
func chromeContext() (context.Context, context.CancelFunc) {
	if os.Getenv("CHROMIUM_NO_SANDBOX") != "true" {
		return chromedp.NewContext(context.Background())
	}
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.NoSandbox, chromedp.Flag("disable-dev-shm-usage", true))
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	ctx, cancel := chromedp.NewContext(allocator)
	return ctx, func() {
		cancel()
		cancelAllocator()
	}
}

// The thumbnails: a map of ThumbnailWidth x ThumbnailHeight pixels
const (
	ThumbnailWidth  = 300
	ThumbnailHeight = 200
	// TilesTimeout is how long the drawing waits for the map tiles: past it, the trace is drawn without the
	// ones still missing (the tile server is slow or can't be reached)
	TilesTimeout = 15 * time.Second
	// tilesLoaded is what the page logs once the tiles of the map are loaded
	tilesLoaded = "all visible tiles have been loaded"
)

// GenerateThumbnail draws the public trace of an activity on a map, with a headless Chromium. At most
// MaxConcurrentThumbnails are drawn at a time, ErrBusy after waiting QueueTimeout; a drawing taking more
// than ThumbnailTimeout is stopped. An activity without public trace (indoor, or a short loop inside its
// privacy zones) gets a blank thumbnail: no map, it would show where the hidden trace is.
func GenerateThumbnail(activity models.Activity) error {
	path := ThumbnailPath(activity.User.UUID, activity.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0770); err != nil {
		return err
	}
	if len(activity.PublicGpsPoints) < 2 {
		return writeBlankThumbnail(path)
	}

	release, err := thumbnailSlots.acquire(QueueTimeout)
	if err != nil {
		return err
	}
	defer release()

	// only read by Chromium below
	page, err := os.CreateTemp("tmp", "thumbnail-*.html")
	if err != nil {
		return err
	}
	defer os.Remove(page.Name())
	_, err = page.WriteString(thumbnailPage(activity.PublicGpsPoints))
	if closeErr := page.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	pagePath, err := filepath.Abs(page.Name())
	if err != nil {
		return err
	}

	browser, cancelBrowser := chromeContext()
	defer cancelBrowser()
	ctx, cancel := context.WithTimeout(browser, ThumbnailTimeout)
	defer cancel()

	loaded := make(chan struct{}, 1)
	chromedp.ListenTarget(ctx, tilesListener(loaded))
	var screenshot []byte
	err = chromedp.Run(ctx,
		chromedp.Navigate("file://"+filepath.ToSlash(pagePath)),
		waitTiles(loaded, TilesTimeout),
		chromedp.Screenshot("#map", &screenshot, chromedp.NodeVisible),
	)
	if err != nil {
		return err
	}
	return os.WriteFile(path, screenshot, 0660)
}

// thumbnailPage returns the page drawing a trace on a map, logging tilesLoaded once the tiles are loaded
func thumbnailPage(points []types.GpsPoint) string {
	var trace strings.Builder
	trace.WriteString("[")
	for i, point := range points {
		if i > 0 {
			trace.WriteString(",")
		}
		// numbers only: nothing of the activity is written in the page as text
		fmt.Fprintf(&trace, "[%f,%f]", point.Lat, point.Lon)
	}
	trace.WriteString("]")
	return `<html><head>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"
	integrity="sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=" crossorigin=""/>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"
	integrity="sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=" crossorigin=""></script>
</head><body>
<div id="map" style="width: ` + fmt.Sprint(ThumbnailWidth) + `px; height: ` + fmt.Sprint(ThumbnailHeight) + `px; position: absolute;"></div>
<script>
var map = L.map('map', { zoomControl: false, fadeAnimation: false, zoomAnimation: false });
var tiles = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', { maxZoom: 19 });
// before the view is set: setting it starts the loading
tiles.on('load', function() { console.log('` + tilesLoaded + `') });
tiles.addTo(map);
var trace = L.polyline(` + trace.String() + `).addTo(map);
map.fitBounds(trace.getBounds());
</script>
</body></html>
`
}

// tilesListener signals on loaded when the page logs tilesLoaded. It never blocks: Leaflet logs it again
// after each batch of tiles, and chromedp calls the listeners on the event loop of the page, which a
// blocked listener would stop (the screenshot would never come).
func tilesListener(loaded chan<- struct{}) func(ev interface{}) {
	return func(ev interface{}) {
		call, ok := ev.(*runtime.EventConsoleAPICalled)
		if !ok {
			return
		}
		for _, arg := range call.Args {
			if string(arg.Value) == `"`+tilesLoaded+`"` {
				select {
				case loaded <- struct{}{}:
				default: // already signaled
				}
				return
			}
		}
	}
}

// waitTiles waits for the tiles of the map, at most timeout: the trace is then drawn without the missing ones
func waitTiles(loaded <-chan struct{}, timeout time.Duration) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-loaded:
			return nil
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

// writeBlankThumbnail writes a thumbnail without map, the color of an empty one
func writeBlankThumbnail(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, ThumbnailWidth, ThumbnailHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{0xdd, 0xdd, 0xdd, 0xff}}, image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0660)
}

package staticmap

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var tileGreen = color.RGBA{0x40, 0xa0, 0x40, 0xff}

// tileServer serves a plain green tile for any {z}/{x}/{y}, recording the requests
type tileServer struct {
	*httptest.Server
	requests  atomic.Int32
	fail      atomic.Bool
	mu        sync.Mutex
	paths     []string
	userAgent string
	running   atomic.Int32 // the requests being answered
	maxRun    atomic.Int32 // the most requests answered at a time
}

func newTileServer(t *testing.T) *tileServer {
	img := image.NewRGBA(image.Rect(0, 0, TileSize, TileSize))
	draw.Draw(img, img.Bounds(), image.NewUniform(tileGreen), image.Point{}, draw.Src)
	var tile bytes.Buffer
	if err := png.Encode(&tile, img); err != nil {
		t.Fatal(err)
	}
	s := &tileServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		running := s.running.Add(1)
		defer s.running.Add(-1)
		for {
			most := s.maxRun.Load()
			if running <= most || s.maxRun.CompareAndSwap(most, running) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.userAgent = r.UserAgent()
		s.mu.Unlock()
		if s.fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tile.Bytes())
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *tileServer) url() string { return s.URL + "/{z}/{x}/{y}.png" }

// a ride around Grenoble
var trace = []Point{{45.18, 5.72}, {45.19, 5.73}, {45.20, 5.71}, {45.185, 5.70}}

func near(a, b color.Color, tolerance uint32) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	diff := func(x, y uint32) bool { return max(x, y)-min(x, y) <= tolerance*0x101 }
	return diff(r1, r2) && diff(g1, g2) && diff(b1, b2) && diff(a1, a2)
}

func TestProject(t *testing.T) {
	for _, c := range []struct {
		p     Point
		zoom  int
		x, y  float64
		about string
	}{
		{Point{0, 0}, 0, 128, 128, "the center of the world"},
		{Point{0, 0}, 1, 256, 256, "twice as big at the next zoom"},
		{Point{maxLatitude, -180}, 0, 0, 0, "the top left corner"},
		{Point{89.9, 180}, 0, 256, 0, "beyond the projection: on its edge"},
	} {
		x, y := project(c.p, c.zoom)
		if math.Abs(x-c.x) > 1e-6 || math.Abs(y-c.y) > 1e-6 {
			t.Errorf("%s: %v at %d = %.3f, %.3f, want %v, %v", c.about, c.p, c.zoom, x, y, c.x, c.y)
		}
	}
}

func TestFitZoom(t *testing.T) {
	width, height := 276, 176
	zoom := fitZoom(trace, width, height)
	fits := func(z int) bool {
		minX, minY, maxX, maxY := bounds(trace, z)
		return maxX-minX <= float64(width) && maxY-minY <= float64(height)
	}
	if !fits(zoom) || fits(zoom+1) {
		t.Errorf("zoom %d: not the closest where the trace fits", zoom)
	}
	if got := fitZoom([]Point{{45.18, 5.72}}, width, height); got != MaxZoom {
		t.Errorf("a single point: zoom %d, want %d", got, MaxZoom)
	}
	if got := fitZoom([]Point{{60, -170}, {-60, 170}}, width, height); got != 0 {
		t.Errorf("around the world: zoom %d, want 0", got)
	}
}

func TestRender(t *testing.T) {
	server := newTileServer(t)
	dir := t.TempDir()
	tiles := NewTiles(dir, server.url())
	img, missing := Render(context.Background(), tiles, trace, 300, 200)
	if missing != 0 {
		t.Fatalf("%d tiles missing", missing)
	}
	if got := img.Bounds().Size(); got != image.Pt(300, 200) {
		t.Fatalf("size %v", got)
	}
	if server.userAgent != UserAgent {
		t.Errorf("User-Agent %q, want %q (the usage policy of OpenStreetMap)", server.userAgent, UserAgent)
	}
	// the corner away from the trace and the attribution: the tile
	if c := img.At(2, 2); !near(c, tileGreen, 0) {
		t.Errorf("top left corner: %v, want the tile %v", c, tileGreen)
	}
	// the points of the trace are drawn in its color
	zoom := fitZoom(trace, 300-2*Padding, 200-2*Padding)
	minX, minY, maxX, maxY := bounds(trace, zoom)
	originX := int(math.Floor((minX+maxX)/2)) - 150
	originY := int(math.Floor((minY+maxY)/2)) - 100
	for _, p := range trace[1:3] {
		x, y := project(p, zoom)
		px, py := int(x)-originX, int(y)-originY
		if px < Padding-1 || px > 300-Padding+1 || py < Padding-1 || py > 200-Padding+1 {
			t.Errorf("%v at %d,%d: outside the padding", p, px, py)
		}
		if c := img.At(px, py); !near(c, TraceColor, 8) {
			t.Errorf("%v at %d,%d: %v, want the trace %v", p, px, py, c, TraceColor)
		}
	}
	// the attribution: dark text on a light box in the bottom right corner
	dark := 0
	for x := 200; x < 300; x++ {
		for y := 188; y < 200; y++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r < 0x8000 && g < 0x8000 && b < 0x8000 {
				dark++
			}
		}
	}
	if dark < 20 {
		t.Errorf("no attribution: %d dark pixels in the bottom right corner", dark)
	}

	// the tiles are kept: drawn again without the server
	requests := server.requests.Load()
	if _, missing := Render(context.Background(), tiles, trace, 300, 200); missing != 0 || server.requests.Load() != requests {
		t.Errorf("drawn again: %d missing, %d new requests", missing, server.requests.Load()-requests)
	}
	offline := NewTiles(dir, "")
	if _, missing := Render(context.Background(), offline, trace, 300, 200); missing != 0 {
		t.Errorf("from the directory only: %d missing", missing)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*.tmp")); len(entries) != 0 {
		t.Errorf("left behind: %v", entries)
	}
}

// a big map needs many tiles: they are downloaded MaxDownloads at a time (the usage policy of OpenStreetMap)
func TestDownloadsAtATime(t *testing.T) {
	server := newTileServer(t)
	if _, missing := Render(context.Background(), NewTiles(t.TempDir(), server.url()), trace, 1000, 1000); missing != 0 {
		t.Fatalf("%d missing", missing)
	}
	if n := server.requests.Load(); n < 16 {
		t.Fatalf("%d tiles: too few to tell", n)
	}
	if most := server.maxRun.Load(); most > MaxDownloads {
		t.Errorf("%d tiles downloaded at a time, at most %d", most, MaxDownloads)
	}
}

// without tiles (none in the directory, no server): the background, no attribution
func TestRenderWithoutTiles(t *testing.T) {
	for name, tiles := range map[string]*Tiles{"nil": nil, "empty directory": NewTiles(t.TempDir(), "")} {
		img, _ := Render(context.Background(), tiles, trace, 300, 200)
		if c := img.At(299, 199); !near(c, Background, 0) {
			t.Errorf("%s: bottom right corner %v, want the background", name, c)
		}
	}
	img, missing := Render(context.Background(), nil, nil, 300, 200)
	if missing != 0 || !near(img.At(150, 100), Background, 0) {
		t.Errorf("no trace: %d missing, center %v", missing, img.At(150, 100))
	}
}

func TestTileDownloadFails(t *testing.T) {
	server := newTileServer(t)
	server.fail.Store(true)
	tiles := NewTiles(t.TempDir(), server.url())
	ctx := context.Background()
	if _, err := tiles.Tile(ctx, 10, 531, 365); err == nil {
		t.Fatal("no error")
	}
	// not asked again before RetryAfter: a down server isn't hammered at each thumbnail
	server.fail.Store(false)
	if _, err := tiles.Tile(ctx, 10, 531, 365); err == nil || server.requests.Load() != 1 {
		t.Errorf("asked again at once: %v, %d requests", err, server.requests.Load())
	}
	down := newTileServer(t)
	down.fail.Store(true)
	if img, missing := Render(ctx, NewTiles(t.TempDir(), down.url()), trace, 300, 200); missing == 0 {
		t.Error("server down: no tile missing")
	} else if c := img.At(2, 2); !near(c, Background, 0) {
		t.Errorf("missing tile: %v, want the background", c)
	}
}

// a tile past MaxAge is downloaded again; when the server fails, the old one is used
func TestStaleTile(t *testing.T) {
	server := newTileServer(t)
	dir := t.TempDir()
	tiles := NewTiles(dir, server.url())
	ctx := context.Background()
	if _, err := tiles.Tile(ctx, 10, 531, 365); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "10", "531", "365.tile")
	old := time.Now().Add(-MaxAge - time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	server.fail.Store(true)
	if _, err := tiles.Tile(ctx, 10, 531, 365); err != nil {
		t.Errorf("stale tile, server down: %v", err)
	}
	server.fail.Store(false)
	if _, err := NewTiles(dir, server.url()).Tile(ctx, 10, 531, 365); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || time.Since(info.ModTime()) > time.Minute {
		t.Errorf("not downloaded again: %v", err)
	}
	if server.requests.Load() != 3 {
		t.Errorf("%d requests, want 3", server.requests.Load())
	}
}

// stopped before the tiles come: drawn without them
func TestRenderCancelled(t *testing.T) {
	server := newTileServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tiles := NewTiles(t.TempDir(), server.url())
	if _, missing := Render(ctx, tiles, trace, 300, 200); missing == 0 {
		t.Error("cancelled: no tile missing")
	}
	// the server isn't at fault: the tiles are asked again at the next drawing
	if _, missing := Render(context.Background(), tiles, trace, 300, 200); missing != 0 {
		t.Errorf("after a cancelled drawing: %d missing", missing)
	}
}

// the trace crossing itself or turning back stays filled, at its crossings and its joins: overlapping shapes
// don't cancel each other
func TestStrokeOverlaps(t *testing.T) {
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for _, c := range []struct {
		name   string
		points [][2]float32
		filled [][2]int
	}{
		{"cross", [][2]float32{{10, 10}, {90, 90}, {90, 10}, {10, 90}}, [][2]int{{50, 50}, {90, 90}, {90, 10}, {30, 30}}},
		{"turn back", [][2]float32{{10, 50}, {90, 50}, {50, 50}}, [][2]int{{70, 50}, {90, 50}, {50, 50}, {10, 50}}},
		{"zigzag", [][2]float32{{10, 50}, {90, 52}, {10, 54}, {90, 56}}, [][2]int{{90, 52}, {10, 54}, {50, 52}}},
	} {
		img := image.NewRGBA(image.Rect(0, 0, 100, 100))
		draw.Draw(img, img.Bounds(), image.NewUniform(white), image.Point{}, draw.Src)
		stroke(img, c.points, 4, TraceColor)
		for _, p := range c.filled {
			if got := img.At(p[0], p[1]); !near(got, TraceColor, 2) {
				t.Errorf("%s: %v at %v, want the trace", c.name, got, p)
			}
		}
		if got := img.At(50, 5); !near(got, white, 0) {
			t.Errorf("%s: %v away from the trace", c.name, got)
		}
	}
}

// a trace across the 180th meridian asks for the tiles of both sides, within the world
func TestRenderAntimeridian(t *testing.T) {
	server := newTileServer(t)
	_, missing := Render(context.Background(), NewTiles(t.TempDir(), server.url()),
		[]Point{{-16.5, 179.99}, {-16.51, 179.995}}, 300, 200)
	if missing != 0 {
		t.Errorf("%d missing", missing)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	for _, path := range server.paths {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, "/"), ".png"), "/")
		z, _ := strconv.Atoi(parts[0])
		x, _ := strconv.Atoi(parts[1])
		if x < 0 || x >= 1<<z {
			t.Errorf("tile %s outside the world", path)
		}
	}
	if len(server.paths) < 2 {
		t.Errorf("%d tiles", len(server.paths))
	}
}

// draws a real map from OpenStreetMap: MYSPORTWEB_OSM_TEST=<file.png> to look at it
func TestRenderOpenStreetMap(t *testing.T) {
	out := os.Getenv("MYSPORTWEB_OSM_TEST")
	if out == "" {
		t.Skip("MYSPORTWEB_OSM_TEST is not set")
	}
	img, missing := Render(context.Background(), NewTiles(t.TempDir(), DefaultURL), trace, 300, 200)
	if missing != 0 {
		t.Errorf("%d tiles missing", missing)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

// Package staticmap draws a trace on a map made of raster tiles (OpenStreetMap by default), without a browser:
// the tiles are downloaded once into a directory, then pasted and the trace drawn over them in Go.
package staticmap

import (
	"MySportWeb/internal/pkg/metrics"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // some tile servers serve JPEG
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TileSize is the side of a raster tile, in pixels
const TileSize = 256

// DefaultURL is the standard tile layer of OpenStreetMap. Its usage policy
// (https://operations.osmfoundation.org/policies/tiles/) asks for a User-Agent naming the application, the
// tiles kept at least 7 days, and the attribution on the map: Tiles and Render do so.
const DefaultURL = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"

// UserAgent identifies the application to the tile server, with its version (set at the start)
var UserAgent = "MySportWeb (self-hosted sport activity tracker)"

const (
	// MaxAge is how long a downloaded tile is used before being downloaded again; past it, the old one is
	// still used when the download fails
	MaxAge = 30 * 24 * time.Hour
	// RetryAfter is how long a tile whose download failed isn't downloaded again
	RetryAfter = 10 * time.Minute
	// maxTileBytes: a tile above it is refused (a tile of OpenStreetMap is a few tens of KB)
	maxTileBytes = 2 << 20
	// MaxDownloads is the number of tiles downloaded at a time, whatever the number of maps drawn: the
	// usage policy of OpenStreetMap forbids heavy use
	MaxDownloads = 2
)

// ErrNoTile is a tile neither in the directory nor downloadable (no URL)
var ErrNoTile = errors.New("no map tile")

// Tiles gives the raster tiles of a tile server, kept in a directory. It is safe for concurrent use.
type Tiles struct {
	dir    string
	url    string // empty: the tiles of the directory only
	client *http.Client
	// a slot per running download
	downloads chan struct{}

	mu     sync.Mutex
	failed map[string]time.Time // the tiles whose download failed, when
}

// NewTiles returns the tiles of url (see DefaultURL: {z}, {x} and {y} are replaced), kept in dir; with an
// empty url, only the tiles already in dir
func NewTiles(dir, url string) *Tiles {
	return &Tiles{dir: dir, url: url, client: &http.Client{Timeout: 30 * time.Second},
		downloads: make(chan struct{}, MaxDownloads), failed: map[string]time.Time{}}
}

// Downloads tells whether missing tiles are downloaded (false: only the ones of the directory)
func (t *Tiles) Downloads() bool {
	return t.url != ""
}

// Tile returns the tile of a zoom level and coordinates, from the directory when it is recent enough,
// downloaded otherwise; a stale one when the download fails. Counted in metrics.MapTiles by result.
func (t *Tiles) Tile(ctx context.Context, z, x, y int) (image.Image, error) {
	img, result, err := t.tile(ctx, z, x, y)
	if err != nil {
		result = "error"
	}
	metrics.MapTiles.WithLabelValues(result).Inc()
	return img, err
}

// tile is Tile, telling where the tile comes from: cache, download or stale
func (t *Tiles) tile(ctx context.Context, z, x, y int) (image.Image, string, error) {
	key := fmt.Sprintf("%d/%d/%d", z, x, y)
	path := filepath.Join(t.dir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".tile")
	info, statErr := os.Stat(path)
	stored := statErr == nil
	if stored && (t.url == "" || time.Since(info.ModTime()) < MaxAge) {
		img, err := readTile(path)
		return img, "cache", err
	}
	if t.url == "" {
		return nil, "", ErrNoTile
	}

	t.mu.Lock()
	failedAt, failed := t.failed[key]
	t.mu.Unlock()
	var err error
	if failed && time.Since(failedAt) < RetryAfter {
		err = fmt.Errorf("tile %s: download failed %s ago", key, time.Since(failedAt).Round(time.Second))
	} else {
		var img image.Image
		if img, err = t.download(ctx, key, path); err == nil {
			t.mu.Lock()
			delete(t.failed, key)
			t.mu.Unlock()
			return img, "download", nil
		}
		// stopped by the caller: the tile server isn't at fault
		if ctx.Err() == nil {
			t.mu.Lock()
			t.failed[key] = time.Now()
			t.mu.Unlock()
		}
	}
	if stored {
		img, err := readTile(path)
		return img, "stale", err
	}
	return nil, "", err
}

func readTile(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}

// download gets a tile, saves it at path once it decodes as an image, and returns it
func (t *Tiles) download(ctx context.Context, key, path string) (image.Image, error) {
	select {
	case t.downloads <- struct{}{}:
		defer func() { <-t.downloads }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	parts := strings.Split(key, "/")
	url := strings.NewReplacer("{z}", parts[0], "{x}", parts[1], "{y}", parts[2]).Replace(t.url)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", UserAgent)
	response, err := t.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxTileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	if len(data) > maxTileBytes {
		return nil, fmt.Errorf("downloading %s: more than %d bytes", url, maxTileBytes)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0770); err != nil {
		return nil, err
	}
	// written aside then renamed: a failed write never leaves a truncated tile
	tmp, err := os.CreateTemp(filepath.Dir(path), "tile-*.tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return nil, err
	}
	return img, nil
}

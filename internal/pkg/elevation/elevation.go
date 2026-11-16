// Package elevation gives the elevation of the ground at a position, from the tiles of a digital elevation
// model in the SRTM .hgt format: one tile per 1°×1° square, named after its south-west corner (N45E005.hgt),
// a grid of big-endian int16 meters, the first row along the north edge. Tiles of 1 arc-second (3601×3601)
// and of 3 arc-seconds (1201×1201) are read.
//
// The tiles are read from a directory, where the missing ones are downloaded at their first use when a URL
// is given: the request only tells which 1°×1° square is needed, never a position.
package elevation

import (
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultURL is where the tiles are downloaded from by default: the SRTM tiles of the AWS Terrain Tiles
// open dataset, gzipped. {lat} is replaced by the latitude part of the tile name (N45), {name} by the
// whole name (N45E005).
const DefaultURL = "https://elevation-tiles-prod.s3.amazonaws.com/skadi/{lat}/{name}.hgt.gz"

// void is the value of the cells without data
const void = -32768

// RetryAfter is how long a tile whose download failed is considered missing before trying again
const RetryAfter = 10 * time.Minute

// the size in bytes of the tiles of 1 and 3 arc-seconds
var tileSizes = map[int64]int{3601 * 3601 * 2: 3601, 1201 * 1201 * 2: 1201}

// ErrNoTile is a square without tile: the sea, or a tile neither in the directory nor downloadable
var ErrNoTile = errors.New("no elevation tile")

// Source gives ground elevations from the tiles of a directory. It is safe for concurrent use.
type Source struct {
	dir    string
	url    string // empty: no download
	client *http.Client

	mu    sync.Mutex
	tiles map[string]*tile
}

type tile struct {
	mu       sync.Mutex
	file     *os.File
	size     int // cells per side
	err      error
	failedAt time.Time
}

// New returns a source reading the tiles of dir, downloading the missing ones from url (see DefaultURL),
// none when url is empty
func New(dir, url string) *Source {
	return &Source{dir: dir, url: url, client: &http.Client{Timeout: 2 * time.Minute}, tiles: map[string]*tile{}}
}

// TileName is the name of the tile holding a position: N45E005 for 45.2°N 5.7°E
func TileName(lat, lon float64) string {
	la, lo := int(math.Floor(lat)), int(math.Floor(lon))
	ns, ew := 'N', 'E'
	if la < 0 {
		ns, la = 'S', -la
	}
	if lo < 0 {
		ew, lo = 'W', -lo
	}
	return fmt.Sprintf("%c%02d%c%03d", ns, la, ew, lo)
}

// At returns the elevation of the ground in meters at a position, interpolated between the 4 cells around
// it; NaN when it is unknown: no tile, a void cell, an invalid position
func (s *Source) At(lat, lon float64) float64 {
	if math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat >= 90 || lon < -180 || lon >= 180 {
		return math.NaN()
	}
	t, err := s.tile(TileName(lat, lon))
	if err != nil {
		return math.NaN()
	}
	last := float64(t.size - 1)
	// the rows go south from the north edge, floor(lat)+1
	x := (lon - math.Floor(lon)) * last
	y := (math.Floor(lat) + 1 - lat) * last
	col, row := int(x), int(y)
	col1, row1 := min(col+1, t.size-1), min(row+1, t.size-1)
	fx, fy := x-float64(col), y-float64(row)

	cells := [4]float64{}
	for i, cell := range [4][2]int{{row, col}, {row, col1}, {row1, col}, {row1, col1}} {
		var b [2]byte
		if _, err := t.file.ReadAt(b[:], int64(cell[0]*t.size+cell[1])*2); err != nil {
			return math.NaN()
		}
		value := int16(binary.BigEndian.Uint16(b[:]))
		if value == void {
			return math.NaN()
		}
		cells[i] = float64(value)
	}
	// bilinear: the nearest cell alone would make a staircase, every step counted as climbing
	north := cells[0]*(1-fx) + cells[1]*fx
	south := cells[2]*(1-fx) + cells[3]*fx
	return north*(1-fy) + south*fy
}

// tile returns the open tile of a name, opening or downloading it at its first use
func (s *Source) tile(name string) (*tile, error) {
	s.mu.Lock()
	t, found := s.tiles[name]
	if !found {
		t = &tile{}
		s.tiles[name] = t
	}
	s.mu.Unlock()

	// one load at a time per tile: the other analyses needing it wait for it
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file != nil {
		return t, nil
	}
	if t.err != nil && (errors.Is(t.err, ErrNoTile) || time.Since(t.failedAt) < RetryAfter) {
		return nil, t.err
	}
	t.err = s.open(t, name)
	if t.err != nil {
		t.failedAt = time.Now()
		return nil, t.err
	}
	return t, nil
}

func (s *Source) open(t *tile, name string) error {
	path := filepath.Join(s.dir, name+".hgt")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if s.url == "" {
			return ErrNoTile
		}
		if err := s.download(name, path); err != nil {
			return err
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	size, ok := tileSizes[info.Size()]
	if !ok {
		file.Close()
		return fmt.Errorf("%s: %d bytes, not a tile of 1 or 3 arc-seconds", path, info.Size())
	}
	t.file, t.size = file, size
	return nil
}

// download saves the tile of a name at path, ErrNoTile when the server has none (the sea)
func (s *Source) download(name, path string) error {
	url := strings.NewReplacer("{lat}", name[:3], "{name}", name).Replace(s.url)
	response, err := s.client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusForbidden:
		// S3 answers 403 for a missing key of a bucket that can't be listed
		return ErrNoTile
	case response.StatusCode != http.StatusOK:
		return fmt.Errorf("downloading %s: %s", url, response.Status)
	}
	var body io.Reader = response.Body
	if strings.HasSuffix(url, ".gz") {
		unzipped, err := gzip.NewReader(response.Body)
		if err != nil {
			return fmt.Errorf("downloading %s: %w", url, err)
		}
		defer unzipped.Close()
		body = unzipped
	}
	if err := os.MkdirAll(s.dir, 0770); err != nil {
		return err
	}
	// written aside then renamed: a failed download never leaves a truncated tile
	tmp, err := os.CreateTemp(s.dir, name+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	// no tile is bigger than one of 1 arc-second
	written, err := io.Copy(tmp, io.LimitReader(body, 3601*3601*2+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	if _, ok := tileSizes[written]; !ok {
		return fmt.Errorf("downloading %s: %d bytes, not a tile of 1 or 3 arc-seconds", url, written)
	}
	return os.Rename(tmp.Name(), path)
}

// Close closes the open tiles
func (s *Source) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, t := range s.tiles {
		t.mu.Lock()
		if t.file != nil {
			t.file.Close()
		}
		t.mu.Unlock()
		delete(s.tiles, name)
	}
}

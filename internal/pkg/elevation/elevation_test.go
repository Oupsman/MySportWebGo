package elevation

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

const size = 1201

// a tile of 3 arc-seconds whose cell (row, col) is 1000 + col - row: rising east, falling north
// to south... rows go south, so the elevation falls going south
func testTile(voidRow int) []byte {
	cells := make([]int16, size*size)
	for row := 0; row < size; row++ {
		for col := 0; col < size; col++ {
			cells[row*size+col] = int16(1000 + col - row)
			if row == voidRow {
				cells[row*size+col] = void
			}
		}
	}
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, cells)
	return b.Bytes()
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

func TestTileName(t *testing.T) {
	for _, c := range []struct {
		lat, lon float64
		want     string
	}{
		{45.2, 5.7, "N45E005"},
		{45, 5, "N45E005"},
		{-0.5, -0.5, "S01W001"},
		{-33.9, 151.2, "S34E151"},
		{40.7, -74.0, "N40W074"},
	} {
		if got := TileName(c.lat, c.lon); got != c.want {
			t.Errorf("TileName(%v, %v) = %s, want %s", c.lat, c.lon, got, c.want)
		}
	}
}

func TestAt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "N45E005.hgt"), testTile(-1), 0600); err != nil {
		t.Fatal(err)
	}
	source := New(dir, "")
	defer source.Close()
	step := 1.0 / (size - 1)
	for _, c := range []struct {
		name     string
		lat, lon float64
		want     float64
	}{
		{"south-west corner", 45, 5, 1000 - (size - 1)},
		{"north-west corner", 46 - step/1e6, 5, 1000},
		{"south-east corner", 45, 6 - step/1e6, 1000},
		{"a cell", 46 - 10*step, 5 + 20*step, 1000 + 20 - 10},
		// between 4 cells: interpolated, not the nearest one
		{"between cells", 46 - 10.5*step, 5 + 20.25*step, 1000 + 20.25 - 10.5},
	} {
		if got := source.At(c.lat, c.lon); math.Abs(got-c.want) > 1e-3 {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	for name, p := range map[string][2]float64{"no tile": {44.5, 5.5}, "NaN": {math.NaN(), 5.5}, "pole": {90, 5}} {
		if got := source.At(p[0], p[1]); !math.IsNaN(got) {
			t.Errorf("%s: %v, want NaN", name, got)
		}
	}
}

func TestVoid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "N45E005.hgt"), testTile(100), 0600); err != nil {
		t.Fatal(err)
	}
	source := New(dir, "")
	step := 1.0 / (size - 1)
	if got := source.At(46-100.5*step, 5.5); !math.IsNaN(got) {
		t.Errorf("next to a void cell: %v, want NaN", got)
	}
	if got := source.At(46-200*step, 5.5); math.IsNaN(got) {
		t.Error("far from the void cells: NaN")
	}
}

func TestInvalidTile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "N45E005.hgt"), []byte("not a tile"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := New(dir, "").At(45.5, 5.5); !math.IsNaN(got) {
		t.Errorf("truncated tile: %v, want NaN", got)
	}
}

func TestDownload(t *testing.T) {
	var gzipped bytes.Buffer
	w := gzip.NewWriter(&gzipped)
	w.Write(testTile(-1))
	w.Close()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/skadi/N45/N45E005.hgt.gz":
			w.Write(gzipped.Bytes())
		case "/skadi/N46/N46E005.hgt.gz":
			w.Write([]byte("truncated"))
		default: // the sea
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "dem") // created at the first download
	source := New(dir, server.URL+"/skadi/{lat}/{name}.hgt.gz")
	defer source.Close()
	if got := source.At(45, 5); !near(got, 1000-(size-1)) {
		t.Errorf("downloaded tile: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "N45E005.hgt")); err != nil {
		t.Errorf("tile not kept: %v", err)
	}
	// the same tile again, from the open file
	source.At(45.5, 5.5)
	if n := requests.Load(); n != 1 {
		t.Errorf("%d requests for one tile", n)
	}

	// the sea: asked once, no file
	for range 3 {
		if got := source.At(44.5, 5.5); !math.IsNaN(got) {
			t.Errorf("sea: %v, want NaN", got)
		}
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("%d requests after the sea, want 2: a missing tile is asked once", n)
	}
	// a broken download leaves nothing behind
	if got := source.At(46.5, 5.5); !math.IsNaN(got) {
		t.Errorf("broken download: %v, want NaN", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("files in the tiles directory: %v, want only N45E005.hgt", entries)
	}

	// another source on the same directory reads the kept tile without downloading it
	if got := New(dir, server.URL+"/skadi/{lat}/{name}.hgt.gz").At(45, 5); !near(got, 1000-(size-1)) || requests.Load() != 3 {
		t.Errorf("kept tile: %v after %d requests", got, requests.Load())
	}
}

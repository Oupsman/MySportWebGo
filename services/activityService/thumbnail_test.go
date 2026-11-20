package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/staticmap"
	"MySportWeb/internal/pkg/types"
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func readThumbnail(t *testing.T, path string) image.Image {
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
	return img
}

func sameColor(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// without public trace, a blank thumbnail: no map around the hidden trace, and no tile downloaded
func TestBlankThumbnail(t *testing.T) {
	t.Chdir(t.TempDir())
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()
	saved := MapTiles
	MapTiles = staticmap.NewTiles(t.TempDir(), server.URL+"/{z}/{x}/{y}.png")
	defer func() { MapTiles = saved }()

	user := models.Users{UUID: uuid.New()}
	for _, points := range []types.GpsPointArray{nil, {{Lat: 45.1, Lon: 5.7}}} {
		activity := models.Activity{ID: uuid.New(), User: user, PublicGpsPoints: points}
		if err := GenerateThumbnail(activity); err != nil {
			t.Fatalf("%d points: %v", len(points), err)
		}
		img := readThumbnail(t, ThumbnailPath(user.UUID, activity.ID))
		if c := img.At(ThumbnailWidth/2, ThumbnailHeight/2); !sameColor(c, staticmap.Background) {
			t.Errorf("%d points: %v in the middle, want blank", len(points), c)
		}
	}
	if requests.Load() != 0 {
		t.Errorf("%d tiles downloaded for blank thumbnails", requests.Load())
	}
}

// the public trace on the tiles of the tile server
func TestThumbnail(t *testing.T) {
	t.Chdir(t.TempDir())
	water := color.RGBA{0xaa, 0xd3, 0xdf, 0xff}
	tile := image.NewRGBA(image.Rect(0, 0, staticmap.TileSize, staticmap.TileSize))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(water), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, tile); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(encoded.Bytes())
	}))
	defer server.Close()
	saved := MapTiles
	MapTiles = staticmap.NewTiles(t.TempDir(), server.URL+"/{z}/{x}/{y}.png")
	defer func() { MapTiles = saved }()

	user := models.Users{UUID: uuid.New()}
	activity := models.Activity{ID: uuid.New(), User: user,
		PublicGpsPoints: types.GpsPointArray{{Lat: 45.18, Lon: 5.72}, {Lat: 45.19, Lon: 5.73}, {Lat: 45.2, Lon: 5.71}}}
	if err := GenerateThumbnail(activity); err != nil {
		t.Fatal(err)
	}
	img := readThumbnail(t, ThumbnailPath(user.UUID, activity.ID))
	if c := img.At(2, 2); !sameColor(c, water) {
		t.Errorf("top left corner: %v, want the tile %v", c, water)
	}
	traced := 0
	for x := range ThumbnailWidth {
		for y := range ThumbnailHeight {
			if sameColor(img.At(x, y), staticmap.TraceColor) {
				traced++
			}
		}
	}
	if traced < 200 {
		t.Errorf("%d pixels of the trace", traced)
	}
}

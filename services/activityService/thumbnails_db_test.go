package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/staticmap"
	"MySportWeb/internal/pkg/types"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var waterGreen = color.RGBA{0x40, 0xa0, 0x40, 0xff}

var twoPoints = types.GpsPointArray{{Lat: 45.18, Lon: 5.72}, {Lat: 45.19, Lon: 5.73}}

// the lists tell which activities have a map on their thumbnail: two points of public trace at least
func TestHasMap(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "maps", Email: "maps@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]bool{}
	for i, points := range []types.GpsPointArray{twoPoints, twoPoints[:1], {}, nil} {
		activity := models.Activity{ID: uuid.New(), UserID: user.ID, Sport: "cycling", PublicGpsPoints: points,
			Date: time.Date(2026, 1, 1+i, 8, 0, 0, 0, time.UTC)}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
		want[activity.ID] = len(points) >= 2
	}
	search, err := db.SearchActivities(user.ID, models.ActivityFilter{}, "date", true, 1, 10, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := db.GetDashboard(user.ID, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Activities) != 4 || len(dashboard.Recent) != 4 {
		t.Fatalf("%d found, %d recent", len(search.Activities), len(dashboard.Recent))
	}
	for i := range 4 {
		if row := search.Activities[i]; row.HasMap != want[row.ID] {
			t.Errorf("search: %s has a map %v, want %v", row.ID, row.HasMap, want[row.ID])
		}
		if recent := dashboard.Recent[i]; recent.HasMap != want[recent.ID] {
			t.Errorf("dashboard: %s has a map %v, want %v", recent.ID, recent.HasMap, want[recent.ID])
		}
	}
}

// green tiles, counting the requests; failing on demand
func greenTiles(t *testing.T) (*staticmap.Tiles, *atomic.Int32, *atomic.Bool) {
	tile := image.NewRGBA(image.Rect(0, 0, staticmap.TileSize, staticmap.TileSize))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(waterGreen), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, tile); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if fail.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(encoded.Bytes())
	}))
	t.Cleanup(server.Close)
	return staticmap.NewTiles(t.TempDir(), server.URL+"/{z}/{x}/{y}.png"), &requests, &fail
}

// the thumbnails drawn by an older version are drawn again, once; not while the tile server is down
func TestBackfillThumbnails(t *testing.T) {
	db := emptyDB(t)
	t.Chdir(t.TempDir())
	saved := MapTiles
	defer func() { MapTiles = saved }()
	var fail *atomic.Bool
	MapTiles, _, fail = greenTiles(t)

	user := models.Users{Username: "thumbs", Email: "thumbs@test.local", UUID: uuid.New()}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	// the most recent first: the outdoor one, whose tiles fail below
	add := func(points types.GpsPointArray, version int, age time.Duration) models.Activity {
		activity := models.Activity{ID: uuid.New(), UserID: user.ID, Sport: "cycling", PublicGpsPoints: points,
			ThumbnailVersion: version, Date: time.Now().Add(-age)}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
		return activity
	}
	outdoor := add(twoPoints, 0, 0)
	indoor := add(nil, 0, time.Hour)
	current := add(twoPoints, ThumbnailVersion, 2*time.Hour)

	// the tile server down: nothing recorded, drawn again at the next start
	fail.Store(true)
	if err := BackfillThumbnails(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if ids, _ := db.GetThumbnailsBelowVersion(ThumbnailVersion); len(ids) != 2 {
		t.Fatalf("tile server down: %d thumbnails left, want 2", len(ids))
	}

	fail.Store(false)
	MapTiles, _, _ = greenTiles(t) // a new cache: the failures are forgotten
	if err := BackfillThumbnails(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if ids, _ := db.GetThumbnailsBelowVersion(ThumbnailVersion); len(ids) != 0 {
		t.Errorf("%d thumbnails left", len(ids))
	}
	for _, activity := range []models.Activity{outdoor, indoor} {
		if _, err := os.Stat(ThumbnailPath(user.UUID, activity.ID)); err != nil {
			t.Errorf("thumbnail of %s: %v", activity.ID, err)
		}
	}
	if _, err := os.Stat(ThumbnailPath(user.UUID, current.ID)); err == nil {
		t.Error("an up to date thumbnail drawn again")
	}
}

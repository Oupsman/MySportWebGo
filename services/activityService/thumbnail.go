package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/staticmap"
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"time"
)

// MapTiles are the map tiles of the thumbnails, set at the start (see cmd/mysportweb); nil: the trace is
// drawn on a plain background (the tests)
var MapTiles *staticmap.Tiles

// The thumbnails: a map of ThumbnailWidth x ThumbnailHeight pixels
const (
	ThumbnailWidth  = 300
	ThumbnailHeight = 200
	// ThumbnailVersion is the version of their drawing, recorded for each activity: the thumbnails of an
	// older one are drawn again at the start (BackfillThumbnails). 1: drawn in Go, with the attribution of
	// the map data (0: by Chromium, without).
	ThumbnailVersion = 1
	// TilesTimeout is how long the drawing waits for the map tiles: past it, the trace is drawn without the
	// ones still missing (the tile server is slow or can't be reached)
	TilesTimeout = 15 * time.Second
)

// GenerateThumbnail draws the public trace of an activity on a map. At most MaxConcurrentThumbnails are
// drawn at a time (they download tiles), ErrBusy after waiting QueueTimeout. An activity without public
// trace (indoor, or a short loop inside its privacy zones) gets a blank thumbnail: no map, it would show
// where the hidden trace is.
func GenerateThumbnail(activity models.Activity) error {
	_, err := drawThumbnail(activity)
	return err
}

// drawThumbnail draws the thumbnail of an activity, see GenerateThumbnail; missing is the number of map tiles
// that couldn't be obtained (left blank)
func drawThumbnail(activity models.Activity) (missing int, err error) {
	path := ThumbnailPath(activity.User.UUID, activity.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0770); err != nil {
		return 0, err
	}
	if len(activity.PublicGpsPoints) < 2 {
		return 0, writeBlankThumbnail(path)
	}

	release, err := thumbnailSlots.acquire(QueueTimeout)
	if err != nil {
		metrics.Thumbnails.WithLabelValues("busy").Inc()
		return 0, err
	}
	defer release()
	start := time.Now()
	defer func() {
		result := "ok"
		if err != nil {
			result = "error"
		}
		metrics.Thumbnails.WithLabelValues(result).Inc()
		metrics.ThumbnailDuration.Observe(time.Since(start).Seconds())
	}()

	trace := make([]staticmap.Point, len(activity.PublicGpsPoints))
	for i, p := range activity.PublicGpsPoints {
		trace[i] = staticmap.Point{Lat: p.Lat, Lon: p.Lon}
	}
	ctx, cancel := context.WithTimeout(context.Background(), TilesTimeout)
	defer cancel()
	img, missing := staticmap.Render(ctx, MapTiles, trace, ThumbnailWidth, ThumbnailHeight)
	return missing, writePNG(path, img)
}

// writeBlankThumbnail writes a thumbnail without map, the color of an empty one
func writeBlankThumbnail(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, ThumbnailWidth, ThumbnailHeight))
	draw.Draw(img, img.Bounds(), image.NewUniform(staticmap.Background), image.Point{}, draw.Src)
	return writePNG(path, img)
}

// writePNG writes an image as a PNG file
func writePNG(path string, img image.Image) error {
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0660)
}

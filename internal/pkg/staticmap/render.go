package staticmap

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Point is a position, in degrees
type Point struct {
	Lat, Lon float64
}

const (
	// MaxZoom is the closest zoom level of a map: a trace of a few meters isn't drawn closer
	MaxZoom = 17
	// Padding is the margin left around the trace, in pixels
	Padding = 12
	// Attribution is the credit of the map data, required by its license (ODbL)
	Attribution = "© OpenStreetMap contributors"
	// maxLatitude is the limit of the Web Mercator projection
	maxLatitude = 85.05112878
)

var (
	// Background is the color of a map without its tiles (the color of an empty Leaflet map)
	Background = color.RGBA{0xdd, 0xdd, 0xdd, 0xff}
	// TraceColor is the color of the trace (the default one of a Leaflet polyline)
	TraceColor = color.RGBA{0x33, 0x88, 0xff, 0xff}
	// haloColor outlines the trace, readable on dark tiles (forests, water)
	haloColor = color.NRGBA{0xff, 0xff, 0xff, 0xcc}
)

// Render draws a trace on a map of width x height pixels, at the closest zoom level where the trace fits.
// The tiles not obtained before ctx is done (or without tiles: nil) are left in the Background color;
// missing is their number. The attribution is written once a tile is drawn.
func Render(ctx context.Context, tiles *Tiles, trace []Point, width, height int) (img *image.RGBA, missing int) {
	img = image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(Background), image.Point{}, draw.Src)
	if len(trace) == 0 {
		return img, 0
	}
	zoom := fitZoom(trace, width-2*Padding, height-2*Padding)

	// the top left corner of the image, in pixels of the world at that zoom
	minX, minY, maxX, maxY := bounds(trace, zoom)
	originX := int(math.Floor((minX+maxX)/2)) - width/2
	originY := int(math.Floor((minY+maxY)/2)) - height/2

	drawn := 0
	if tiles != nil {
		drawn, missing = drawTiles(ctx, img, tiles, zoom, originX, originY)
	}

	points := make([][2]float32, 0, len(trace))
	for _, p := range trace {
		x, y := project(p, zoom)
		point := [2]float32{float32(x - float64(originX)), float32(y - float64(originY))}
		// closer than a pixel to the previous point: nothing to see, and less to rasterize
		if n := len(points); n > 0 && math.Hypot(float64(point[0]-points[n-1][0]), float64(point[1]-points[n-1][1])) < 1 {
			continue
		}
		points = append(points, point)
	}
	stroke(img, points, 6, haloColor)
	stroke(img, points, 3.5, TraceColor)

	if drawn > 0 {
		writeAttribution(img)
	}
	return img, missing
}

// project returns the pixel of a position in the Web Mercator world at a zoom level, TileSize * 2^zoom
// pixels wide
func project(p Point, zoom int) (x, y float64) {
	scale := TileSize * math.Exp2(float64(zoom))
	lat := math.Max(-maxLatitude, math.Min(maxLatitude, p.Lat))
	sin := math.Sin(lat * math.Pi / 180)
	x = (p.Lon + 180) / 360 * scale
	y = (0.5 - math.Log((1+sin)/(1-sin))/(4*math.Pi)) * scale
	return x, y
}

// bounds returns the pixels of the corners of the box around a trace at a zoom level
func bounds(trace []Point, zoom int) (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, p := range trace {
		x, y := project(p, zoom)
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return minX, minY, maxX, maxY
}

// fitZoom returns the closest zoom level where the trace fits in width x height pixels, 0 at worst
func fitZoom(trace []Point, width, height int) int {
	// the size of the trace doubles at each zoom level
	minX, minY, maxX, maxY := bounds(trace, 0)
	for zoom := MaxZoom; zoom > 0; zoom-- {
		scale := math.Exp2(float64(zoom))
		if (maxX-minX)*scale <= float64(width) && (maxY-minY)*scale <= float64(height) {
			return zoom
		}
	}
	return 0
}

// drawTiles pastes the tiles under the image, its top left corner at originX, originY in the world. It
// returns how many were drawn, and how many are missing.
func drawTiles(ctx context.Context, img *image.RGBA, tiles *Tiles, zoom, originX, originY int) (drawn, missing int) {
	size := img.Bounds().Size()
	n := 1 << zoom
	type placed struct {
		tile image.Image
		at   image.Point
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		got []placed
	)
	for ty := floorDiv(originY, TileSize); ty <= floorDiv(originY+size.Y-1, TileSize); ty++ {
		if ty < 0 || ty >= n {
			continue // above or below the world: background
		}
		for tx := floorDiv(originX, TileSize); tx <= floorDiv(originX+size.X-1, TileSize); tx++ {
			at := image.Pt(tx*TileSize-originX, ty*TileSize-originY)
			// the world wraps around the 180th meridian
			x := ((tx % n) + n) % n
			wg.Go(func() {
				tile, err := tiles.Tile(ctx, zoom, x, ty)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					missing++
					return
				}
				got = append(got, placed{tile, at})
			})
		}
	}
	wg.Wait()
	for _, p := range got {
		draw.Draw(img, image.Rectangle{Min: p.at, Max: p.at.Add(image.Pt(TileSize, TileSize))}, p.tile,
			p.tile.Bounds().Min, draw.Src)
	}
	return len(got), missing
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// stroke draws a line through points, width pixels wide, with round joins and ends. Every shape is
// wound the same way: the rasterizer merges overlapping shapes of the same winding, it would cut out
// overlapping shapes of opposite windings.
func stroke(img *image.RGBA, points [][2]float32, width float32, c color.Color) {
	size := img.Bounds().Size()
	r := vector.NewRasterizer(size.X, size.Y)
	r.DrawOp = draw.Over
	half := width / 2
	for i, p := range points {
		disc(r, p, half)
		if i == 0 {
			continue
		}
		q := points[i-1]
		dx, dy := p[0]-q[0], p[1]-q[1]
		length := float32(math.Hypot(float64(dx), float64(dy)))
		if length == 0 {
			continue
		}
		// the normal, half the width long
		nx, ny := -dy/length*half, dx/length*half
		r.MoveTo(q[0]+nx, q[1]+ny)
		r.LineTo(p[0]+nx, p[1]+ny)
		r.LineTo(p[0]-nx, p[1]-ny)
		r.LineTo(q[0]-nx, q[1]-ny)
		r.ClosePath()
	}
	r.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
}

// disc adds a disc to the path, wound as the segments of stroke
func disc(r *vector.Rasterizer, center [2]float32, radius float32) {
	const sides = 16
	for i := range sides {
		angle := -2 * math.Pi * float64(i) / sides
		x := center[0] + radius*float32(math.Cos(angle))
		y := center[1] + radius*float32(math.Sin(angle))
		if i == 0 {
			r.MoveTo(x, y)
		} else {
			r.LineTo(x, y)
		}
	}
	r.ClosePath()
}

var attributionFace = sync.OnceValues(func() (font.Face, error) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{Size: 9, DPI: 72, Hinting: font.HintingFull})
})

// writeAttribution writes Attribution in the bottom right corner, on a light box
func writeAttribution(img *image.RGBA) {
	face, err := attributionFace()
	if err != nil {
		return // the font is compiled in: never
	}
	drawer := font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{0x33, 0x33, 0x33, 0xff}), Face: face}
	const margin = 3
	textWidth := drawer.MeasureString(Attribution).Ceil()
	metrics := face.Metrics()
	b := img.Bounds()
	box := image.Rect(b.Max.X-textWidth-2*margin, b.Max.Y-metrics.Height.Ceil()-margin, b.Max.X, b.Max.Y)
	draw.Draw(img, box, image.NewUniform(color.NRGBA{0xff, 0xff, 0xff, 0xb3}), image.Point{}, draw.Over)
	drawer.Dot = fixed.P(box.Min.X+margin, b.Max.Y-metrics.Descent.Ceil()-margin/2)
	drawer.DrawString(Attribution)
}

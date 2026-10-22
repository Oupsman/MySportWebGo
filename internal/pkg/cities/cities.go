// Package cities finds the city a GPS point is in, from the GeoNames cities of more than 15000
// inhabitants (CC BY 4.0, https://www.geonames.org/). See gen to rebuild the data.
package cities

import (
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"math"
	"strconv"
	"strings"
	"sync"
)

// MaxDistanceKm is the max distance between a point and the center of its city
const MaxDistanceKm = 10.0

//go:embed cities15000.tsv.gz
var data []byte

type city struct {
	name string
	types.GpsPoint
}

var (
	loadOnce sync.Once
	all      []city
)

// load reads the embedded cities, the data being built by gen it can't be invalid
func load() {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		panic("cities: " + err.Error())
	}
	scanner := bufio.NewScanner(zr)
	for scanner.Scan() {
		cols := strings.Split(scanner.Text(), "\t")
		if len(cols) != 3 {
			panic("cities: invalid line " + scanner.Text())
		}
		lat, errLat := strconv.ParseFloat(cols[1], 64)
		lon, errLon := strconv.ParseFloat(cols[2], 64)
		if errLat != nil || errLon != nil {
			panic("cities: invalid line " + scanner.Text())
		}
		all = append(all, city{cols[0], types.GpsPoint{Lat: lat, Lon: lon}})
	}
	if err := scanner.Err(); err != nil {
		panic("cities: " + err.Error())
	}
}

// Nearest returns the name of the city whose center (or one of its districts' ones) is the nearest
// to p, "" when there is none within MaxDistanceKm
func Nearest(p types.GpsPoint) string {
	loadOnce.Do(load)
	// a degree of latitude is 111 km: farther cities are skipped without computing their distance
	maxLat := MaxDistanceKm / 111
	name, best := "", math.Inf(1)
	for _, c := range all {
		if math.Abs(c.Lat-p.Lat) > maxLat {
			continue
		}
		if d := utils.Haversine(p, c.GpsPoint); d <= MaxDistanceKm && d < best {
			name, best = c.name, d
		}
	}
	return name
}

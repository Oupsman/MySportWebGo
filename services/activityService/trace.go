package activityService

import (
	"MySportWeb/internal/pkg/types"
	"math"
)

func PerpendicularDistance(p, start, end types.GpsPoint) float64 {
	if start.Lat == end.Lat && start.Lon == end.Lon {
		return math.Sqrt((p.Lat-start.Lat)*(p.Lat-start.Lat) + (p.Lon-start.Lon)*(p.Lon-start.Lon))
	}

	num := math.Abs((end.Lon-start.Lon)*p.Lat - (end.Lat-start.Lat)*p.Lon + end.Lat*start.Lon - end.Lon*start.Lat)
	den := math.Sqrt((end.Lon-start.Lon)*(end.Lon-start.Lon) + (end.Lat-start.Lat)*(end.Lat-start.Lat))
	return num / den
}

// DouglasPeucker Simplify the GPS trace using Douglas Peucker Algorithm
func DouglasPeucker(points []types.GpsPoint, epsilon float64) []types.GpsPoint {
	if len(points) < 2 {
		return points
	}

	dmaLat := 0.0
	indeLat := 0
	for i := 1; i < len(points)-1; i++ {
		d := PerpendicularDistance(points[i], points[0], points[len(points)-1])
		if d > dmaLat {
			indeLat = i
			dmaLat = d
		}
	}

	if dmaLat > epsilon {
		recResults1 := DouglasPeucker(points[:indeLat+1], epsilon)
		recResults2 := DouglasPeucker(points[indeLat:], epsilon)

		return append(recResults1[:len(recResults1)-1], recResults2...)
	} else {
		return []types.GpsPoint{points[0], points[len(points)-1]}
	}
}

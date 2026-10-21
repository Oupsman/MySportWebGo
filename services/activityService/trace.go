package activityService

import (
	"MySportWeb/internal/pkg/types"
	"math"
)

const earthRadiusMeters = 6371000.0

// SimplifyEpsilon is the max deviation (in meters) tolerated when simplifying a GPS trace
const SimplifyEpsilon = 10.0

// point2D is a GPS point projected on a local plane, in meters
type point2D struct {
	X, Y float64
}

// project converts GPS points to a local equirectangular plane centered on refLat.
// Accurate enough (<0.1% error) for the extent of a sport activity.
func project(points []types.GpsPoint, refLat float64) []point2D {
	cosRef := math.Cos(refLat * math.Pi / 180)
	projected := make([]point2D, len(points))
	for i, p := range points {
		projected[i] = point2D{
			X: earthRadiusMeters * p.Lon * math.Pi / 180 * cosRef,
			Y: earthRadiusMeters * p.Lat * math.Pi / 180,
		}
	}
	return projected
}

// segmentDistance returns the distance between p and the segment [a, b]
func segmentDistance(p, a, b point2D) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	lengthSq := dx*dx + dy*dy
	if lengthSq == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / lengthSq
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

// DouglasPeucker Simplify the GPS trace using Douglas Peucker Algorithm, epsilon is in meters
func DouglasPeucker(points []types.GpsPoint, epsilon float64) []types.GpsPoint {
	if len(points) < 3 {
		return points
	}
	projected := project(points, points[0].Lat)
	keep := make([]bool, len(points))
	keep[0], keep[len(points)-1] = true, true

	// iterative version, GPS traces can have tens of thousands of points
	stack := [][2]int{{0, len(points) - 1}}
	for len(stack) > 0 {
		start, end := stack[len(stack)-1][0], stack[len(stack)-1][1]
		stack = stack[:len(stack)-1]

		maxDistance := 0.0
		index := 0
		for i := start + 1; i < end; i++ {
			d := segmentDistance(projected[i], projected[start], projected[end])
			if d > maxDistance {
				index = i
				maxDistance = d
			}
		}
		if maxDistance > epsilon {
			keep[index] = true
			stack = append(stack, [2]int{start, index}, [2]int{index, end})
		}
	}

	var simplified []types.GpsPoint
	for i, p := range points {
		if keep[i] {
			simplified = append(simplified, p)
		}
	}
	return simplified
}

// SimplifyTrace returns the simplified trace used to compare activities
func SimplifyTrace(points []types.GpsPoint) types.GpsPointArray {
	return DouglasPeucker(points, SimplifyEpsilon)
}

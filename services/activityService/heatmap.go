package activityService

import (
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
)

// HideEnds cuts the first and the last meters of a trace: where the user lives, out of the screenshots of
// their heatmap. Nil when the trace isn't longer than twice that.
func HideEnds(points []types.GpsPoint, meters float64) []types.GpsPoint {
	if meters <= 0 || len(points) < 2 {
		return points
	}
	cumulated := make([]float64, len(points))
	for i := 1; i < len(points); i++ {
		// utils.Haversine is in km
		cumulated[i] = cumulated[i-1] + utils.Haversine(points[i-1], points[i])*1000
	}
	total := cumulated[len(points)-1]
	if total <= 2*meters {
		return nil
	}
	// the points beyond the first meters and before the last ones, and where the trace crosses those limits
	var kept []types.GpsPoint
	for i := 1; i < len(points); i++ {
		a, b := cumulated[i-1], cumulated[i]
		for _, limit := range []float64{meters, total - meters} {
			if a < limit && b >= limit && b > a {
				f := (limit - a) / (b - a)
				kept = append(kept, types.GpsPoint{Lat: points[i-1].Lat + f*(points[i].Lat-points[i-1].Lat),
					Lon: points[i-1].Lon + f*(points[i].Lon-points[i-1].Lon)})
			}
		}
		if b > meters && b < total-meters {
			kept = append(kept, points[i])
		}
	}
	return kept
}

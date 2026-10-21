package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"math"
)

const (
	// SimilarDistanceTolerance is the max relative difference between two activities' distances
	SimilarDistanceTolerance = 0.10
	// SimilarEndpointsKm is the max distance between the start (and end) points of two activities
	SimilarEndpointsKm = 0.2
	// similarCorridorMeters is the width around a trace inside which the other trace must stay
	similarCorridorMeters = 50.0
	// similarMinCoverage is the share of each trace that must lie inside the other one's corridor
	similarMinCoverage = 0.9
)

// EndpointsMatch checks that both activities start and end at the same place
func EndpointsMatch(startA, endA, startB, endB *types.GpsPoint) bool {
	if startA == nil || endA == nil || startB == nil || endB == nil {
		return false
	}
	return utils.Haversine(*startA, *startB) <= SimilarEndpointsKm &&
		utils.Haversine(*endA, *endB) <= SimilarEndpointsKm
}

// coverage returns the share of trace a (weighted by length) lying within corridor meters of trace b
func coverage(a, b []point2D, corridor float64) float64 {
	var covered, total float64
	for i, p := range a {
		// each vertex stands for half of its adjacent segments
		weight := 0.0
		if i > 0 {
			weight += math.Hypot(p.X-a[i-1].X, p.Y-a[i-1].Y) / 2
		}
		if i < len(a)-1 {
			weight += math.Hypot(a[i+1].X-p.X, a[i+1].Y-p.Y) / 2
		}
		total += weight

		for j := 0; j < len(b)-1; j++ {
			if segmentDistance(p, b[j], b[j+1]) <= corridor {
				covered += weight
				break
			}
		}
	}
	if total == 0 {
		return 0
	}
	return covered / total
}

// TraceSimilarity returns the lowest coverage of one simplified trace by the other, between 0 and 1.
// Direction is not taken into account: a loop done backwards is the same route.
func TraceSimilarity(a, b []types.GpsPoint) float64 {
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	refLat := a[0].Lat
	pa, pb := project(a, refLat), project(b, refLat)
	return math.Min(coverage(pa, pb, similarCorridorMeters), coverage(pb, pa, similarCorridorMeters))
}

// TracesSimilar tells if two simplified traces follow the same route
func TracesSimilar(a, b []types.GpsPoint) bool {
	return TraceSimilarity(a, b) >= similarMinCoverage
}

// DistancesMatch checks that both distances are within SimilarDistanceTolerance of each other
func DistancesMatch(a, b float64) bool {
	return math.Abs(a-b) <= a*SimilarDistanceTolerance
}

// ActivitiesSimilar tells if two activities are the same workout: same sport, distance, endpoints and route.
// Both activities must have their SimplifiedGpsPoints set.
func ActivitiesSimilar(a, b models.Activity) bool {
	return a.Sport == b.Sport &&
		DistancesMatch(a.Distance, b.Distance) &&
		EndpointsMatch(a.StartPosition, a.EndPosition, b.StartPosition, b.EndPosition) &&
		TracesSimilar(a.SimplifiedGpsPoints, b.SimplifiedGpsPoints)
}

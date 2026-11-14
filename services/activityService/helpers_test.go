package activityService

// Used by the tests only: in a test file, they stay out of the compiled code.

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
)

// SumAnalyze analyses a whole FIT file, its training score computed with DefaultRestHR when the device
// didn't record one: see AnalyzeTrimmed for the user's resting heart rate
func SumAnalyze(filePath string, user models.Users, equipment models.Equipments) (models.Activity, error) {
	return AnalyzeTrimmed(filePath, user, equipment, 0, nil)
}

// TracesSimilar tells if two simplified traces follow the same route
func TracesSimilar(a, b []types.GpsPoint) bool {
	return TraceSimilarity(a, b) >= similarMinCoverage
}

// ActivitiesSimilar tells if two activities are the same workout: same sport, distance, endpoints and route.
// Both activities must have their SimplifiedGpsPoints set.
func ActivitiesSimilar(a, b models.Activity) bool {
	return a.Sport == b.Sport &&
		DistancesMatch(a.Distance, b.Distance) &&
		EndpointsMatch(a.StartPosition, a.EndPosition, b.StartPosition, b.EndPosition) &&
		TracesSimilar(a.SimplifiedGpsPoints, b.SimplifiedGpsPoints)
}

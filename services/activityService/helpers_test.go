package activityService

// Used by the tests only: in a test file, they stay out of the compiled code.

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"context"
	"sync"
	"testing"
	"time"
)

// SumAnalyze analyses a whole FIT file, its training score computed with DefaultRestHR when the device
// didn't record one: see AnalyzeTrimmed for the user's resting heart rate
func SumAnalyze(filePath string, user models.Users, equipment models.Equipments) (models.Activity, error) {
	return AnalyzeTrimmed(filePath, user, equipment, 0, Analysis{})
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

// testBackground runs the background work of the tests in a goroutine, never stopped
type testBackground struct{}

func (testBackground) Go(fn func(ctx context.Context)) {
	go fn(context.Background())
}

// fillAnalysisSlots takes every analysis slot, as a burst of uploads does, with QueueTimeout and
// BusyRetryDelay shortened so that analyses are refused with ErrBusy at once. The returned function frees
// the slots (once); the timeouts are restored at the end of the test.
func fillAnalysisSlots(t *testing.T) (release func()) {
	t.Helper()
	savedQueue, savedRetry := QueueTimeout, BusyRetryDelay
	QueueTimeout, BusyRetryDelay = 20*time.Millisecond, 10*time.Millisecond
	var releases []func()
	for range MaxConcurrentAnalyses {
		r, err := analysisSlots.acquire(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, r)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			for _, r := range releases {
				r()
			}
		})
	}
	t.Cleanup(func() {
		release()
		QueueTimeout, BusyRetryDelay = savedQueue, savedRetry
	})
	return release
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"fmt"
	"github.com/google/uuid"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// FIT files are not versioned (see .gitignore), the test is skipped when there are none
const fitFilesPattern = "../../prototypes/*.fit"

// minFitDistance excludes recordings where the device did not move: a zero length trace is similar to nothing
const minFitDistance = 100.0

type fitActivity struct {
	file     string
	activity models.Activity
}

// loadFitActivities runs the import pipeline on every FIT file, skipping the ones it can't analyze
func loadFitActivities(t *testing.T) []fitActivity {
	files, err := filepath.Glob(fitFilesPattern)
	if err != nil {
		t.Fatal(err)
	}
	var activities []fitActivity
	for _, file := range files {
		activity, err := analyzeFitFile(file)
		if err != nil {
			t.Logf("skipping %s: %v", filepath.Base(file), err)
			continue
		}
		if len(activity.SimplifiedGpsPoints) < 2 {
			t.Logf("skipping %s: no GPS trace (%s)", filepath.Base(file), activity.Sport)
			continue
		}
		if activity.Distance < minFitDistance {
			t.Logf("skipping %s: only %.0f m", filepath.Base(file), activity.Distance)
			continue
		}
		activities = append(activities, fitActivity{file: filepath.Base(file), activity: activity})
	}
	return activities
}

// analyzeFitFile wraps SumAnalyze, which panics on some files (no session for instance)
func analyzeFitFile(file string) (activity models.Activity, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return SumAnalyze(file, models.Users{}, models.Equipments{})
}

// sortGroups puts the biggest groups first, then orders by first member
func sortGroups(groups [][]int) {
	for _, group := range groups {
		sort.Ints(group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i]) != len(groups[j]) {
			return len(groups[i]) > len(groups[j])
		}
		return groups[i][0] < groups[j][0]
	})
}

// pairwiseGroups groups activities similar to each other, transitively: A~B and B~C puts A and C together
func pairwiseGroups(activities []fitActivity) ([][]int, []string) {
	parent := make([]int, len(activities))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}

	var nearMisses []string
	for i := range activities {
		a := activities[i].activity
		for j := i + 1; j < len(activities); j++ {
			b := activities[j].activity
			if ActivitiesSimilar(a, b) || ActivitiesSimilar(b, a) {
				parent[find(j)] = find(i)
				continue
			}
			// same sport, distance and endpoints but rejected on the route: useful to tune the thresholds
			if a.Sport == b.Sport && DistancesMatch(a.Distance, b.Distance) &&
				EndpointsMatch(a.StartPosition, a.EndPosition, b.StartPosition, b.EndPosition) {
				nearMisses = append(nearMisses, fmt.Sprintf("  %s <> %s: route similarity %.2f",
					activities[i].file, activities[j].file, TraceSimilarity(a.SimplifiedGpsPoints, b.SimplifiedGpsPoints)))
			}
		}
	}

	byRoot := map[int][]int{}
	for i := range activities {
		byRoot[find(i)] = append(byRoot[find(i)], i)
	}
	var groups [][]int
	for _, group := range byRoot {
		if len(group) > 1 {
			groups = append(groups, group)
		}
	}
	sortGroups(groups)
	return groups, nearMisses
}

// assignRoutes mimics the import: activities are processed oldest first and matched against
// the reference trace of the existing routes, a new route is created when none matches
func assignRoutes(activities []fitActivity) (routeOf []int, references []int) {
	var routes []models.Route
	routeOf = make([]int, len(activities))
	for i, a := range activities {
		route := MatchRoute(a.activity, routes)
		if route == nil {
			newRoute := NewRoute(a.activity)
			newRoute.ID = uuid.New()
			routes = append(routes, newRoute)
			references = append(references, i)
			routeOf[i] = len(routes) - 1
			continue
		}
		for r := range routes {
			if routes[r].ID == route.ID {
				routeOf[i] = r
			}
		}
	}
	return routeOf, references
}

func TestSimilarFitFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("decoding FIT files is slow")
	}
	activities := loadFitActivities(t)
	if len(activities) < 2 {
		t.Skip("not enough FIT files with GPS data in prototypes/")
	}
	sort.SliceStable(activities, func(i, j int) bool { return activities[i].activity.Date.Before(activities[j].activity.Date) })
	t.Logf("%d activities with GPS data", len(activities))

	routeOf, references := assignRoutes(activities)
	membersOf := make([][]int, len(references))
	for i, r := range routeOf {
		membersOf[r] = append(membersOf[r], i)
		reference := activities[references[r]].activity
		if !TracesSimilar(activities[i].activity.SimplifiedGpsPoints, reference.SimplifiedGpsPoints) {
			t.Errorf("%s is not similar to the reference of its route %s", activities[i].file, activities[references[r]].file)
		}
	}
	var routeGroups [][]int
	for _, members := range membersOf {
		if len(members) > 1 {
			routeGroups = append(routeGroups, members)
		}
	}
	sortGroups(routeGroups)

	var report strings.Builder
	fmt.Fprintf(&report, "%d routes, %d with several activities (* is the reference trace)\n", len(references), len(routeGroups))
	for n, group := range routeGroups {
		first := activities[group[0]].activity
		fmt.Fprintf(&report, "route %d: %s, ~%.1f km, %d activities\n", n+1, first.Sport, first.Distance/1000, len(group))
		for _, i := range group {
			a := activities[i].activity
			marker := " "
			if references[routeOf[i]] == i {
				marker = "*"
			}
			fmt.Fprintf(&report, " %s %s  %s  %.2f km  %d -> %d points\n", marker, activities[i].file,
				a.Date.Format("2006-01-02 15:04"), a.Distance/1000, len(a.GpsPoints), len(a.SimplifiedGpsPoints))
		}
	}

	// compare with the pairwise grouping: reference matching does not chain, groups may be split
	pairGroups, nearMisses := pairwiseGroups(activities)
	fmt.Fprintf(&report, "pairwise grouping: %d groups, split by the routes:\n", len(pairGroups))
	for _, group := range pairGroups {
		split := map[int][]string{}
		for _, i := range group {
			split[routeOf[i]] = append(split[routeOf[i]], activities[i].file)
		}
		if len(split) == 1 {
			continue
		}
		fmt.Fprintf(&report, "  %d activities split into %d routes:\n", len(group), len(split))
		var routeIndexes []int
		for r := range split {
			routeIndexes = append(routeIndexes, r)
		}
		sort.Ints(routeIndexes)
		for _, r := range routeIndexes {
			fmt.Fprintf(&report, "    %s\n", strings.Join(split[r], ", "))
		}
	}

	fmt.Fprintf(&report, "%d near misses (same sport, distance and endpoints, different route)\n", len(nearMisses))
	report.WriteString(strings.Join(nearMisses, "\n"))
	t.Log("\n" + report.String())
}

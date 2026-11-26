package activityService

import (
	"MySportWeb/internal/pkg/models"
	"context"
	"math"

	"sort"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// RouteMinDistance is the min distance (in meters) of an activity to get a route:
// below that, the device most likely did not move
const RouteMinDistance = 100.0

// canHaveRoute tells if an activity has a GPS trace long enough to be matched to a route
func canHaveRoute(activity models.Activity) bool {
	return activity.Distance >= RouteMinDistance && len(activity.SimplifiedGpsPoints) >= 2 &&
		activity.StartPosition != nil && activity.EndPosition != nil
}

// RouteMembersCompared is the number of activities of a route, the most recent ones, an activity is compared
// to besides the route's reference: a single reference, taken from the first activity, may be a poor
// recording of the route
const RouteMembersCompared = 5

// routeReference returns the reference trace of a route
func routeReference(route models.Route) models.RouteReference {
	return models.RouteReference{RouteID: route.ID, Distance: route.Distance, StartPosition: route.StartPosition,
		EndPosition: route.EndPosition, SimplifiedGpsPoints: route.SimplifiedGpsPoints}
}

// activityReference returns the trace of an activity, to compare it to route references
func activityReference(activity models.Activity) models.RouteReference {
	routeID := uuid.Nil
	if activity.RouteID != nil {
		routeID = *activity.RouteID
	}
	return models.RouteReference{RouteID: routeID, Distance: activity.Distance, StartPosition: activity.StartPosition,
		EndPosition: activity.EndPosition, SimplifiedGpsPoints: activity.SimplifiedGpsPoints}
}

// referencesSimilarity returns the similarity of two traces, 0 when their distances or endpoints don't match
func referencesSimilarity(a, b models.RouteReference) float64 {
	if !DistancesMatch(a.Distance, b.Distance) ||
		!EndpointsMatch(a.StartPosition, a.EndPosition, b.StartPosition, b.EndPosition) {
		return 0
	}
	return TraceSimilarity(a.SimplifiedGpsPoints, b.SimplifiedGpsPoints)
}

// bestSimilarity returns the best similarity between a trace of as and a trace of bs
func bestSimilarity(as, bs []models.RouteReference) float64 {
	best := 0.0
	for _, a := range as {
		for _, b := range bs {
			best = math.Max(best, referencesSimilarity(a, b))
		}
	}
	return best
}

// referencesByRoute groups the routes' references with their members' traces
func referencesByRoute(routes []models.Route, members []models.RouteReference) map[uuid.UUID][]models.RouteReference {
	references := make(map[uuid.UUID][]models.RouteReference, len(routes))
	for _, route := range routes {
		references[route.ID] = append(references[route.ID], routeReference(route))
	}
	for _, member := range members {
		if _, ok := references[member.RouteID]; ok {
			references[member.RouteID] = append(references[member.RouteID], member)
		}
	}
	return references
}

// MatchRoute returns the route the activity follows with the best similarity, nil if there is none. The
// activity is compared to the reference of each route and to the traces of its members.
func MatchRoute(activity models.Activity, routes []models.Route, members []models.RouteReference) *models.Route {
	references := referencesByRoute(routes, members)
	trace := []models.RouteReference{activityReference(activity)}
	var best *models.Route
	bestSoFar := 0.0
	for i, route := range routes {
		if route.Sport != activity.Sport {
			continue
		}
		similarity := bestSimilarity(trace, references[route.ID])
		if similarity >= similarMinCoverage && similarity > bestSoFar {
			best = &routes[i]
			bestSoFar = similarity
		}
	}
	return best
}

// NewRoute creates a route whose reference trace is the activity's one
func NewRoute(activity models.Activity) models.Route {
	return models.Route{
		UserID:              activity.UserID,
		Name:                activity.Title,
		Sport:               activity.Sport,
		Distance:            activity.Distance,
		StartPosition:       activity.StartPosition,
		EndPosition:         activity.EndPosition,
		SimplifiedGpsPoints: activity.SimplifiedGpsPoints,
	}
}

// assignRoute sets activity.RouteID to the matching route, creating one when none matches.
// It must run in a transaction: the user's routes stay locked until it ends.
func assignRoute(db *models.DB, activity *models.Activity) error {
	if !canHaveRoute(*activity) {
		return nil
	}
	err := db.LockUserRoutes(activity.UserID)
	if err != nil {
		return err
	}
	routes, members, err := db.GetRouteCandidates(activity.UserID, activity.Sport,
		activity.Distance*(1-SimilarDistanceTolerance), activity.Distance*(1+SimilarDistanceTolerance), RouteMembersCompared)
	if err != nil {
		return err
	}
	route := MatchRoute(*activity, routes, members)
	if route == nil {
		newRoute := NewRoute(*activity)
		newRoute.ActivityCount = 1
		err = db.CreateRoute(&newRoute)
		if err != nil {
			return err
		}
		activity.RouteID = &newRoute.ID
		return nil
	}
	err = db.IncrementRouteCount(route.ID)
	if err != nil {
		return err
	}
	activity.RouteID = &route.ID
	if route.IsCommute {
		return markCommute(db, activity)
	}
	return nil
}

// DuplicateError is returned when the user already imported the activity
type DuplicateError struct {
	ExistingID uuid.UUID
}

func (e *DuplicateError) Error() string {
	return "this activity was already imported"
}

// CreateActivity saves a new activity, linked to the route it follows. It refuses duplicates with a
// *DuplicateError. store, when not nil, is called before saving, while the user's imports are serialized:
// it puts the FIT file in place and sets activity.FilePath; it can't be overwritten by a concurrent upload.
func CreateActivity(db *models.DB, activity *models.Activity, store func(activity *models.Activity) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		txDB := &models.DB{DB: *tx}
		// UserID is only filled by gorm when saving the User association
		activity.UserID = activity.User.ID
		// one import at a time per user: two uploads of the same file can't both pass the duplicate check
		if err := txDB.LockUserRoutes(activity.UserID); err != nil {
			return err
		}
		existing, err := txDB.FindDuplicateActivity(activity.UserID, activity.Sport, activity.Date)
		if err != nil {
			return err
		}
		if existing != nil {
			return &DuplicateError{ExistingID: *existing}
		}
		if err := assignRoute(txDB, activity); err != nil {
			return err
		}
		// not on a commute route: a commute all the same when it starts or ends at a workplace
		if err := commuteFromWorkplaces(txDB, activity); err != nil {
			return err
		}
		// a new device starts without correction: the user enables it on the device page
		deviceID, err := txDB.FindOrCreateDevice(activity.UserID, activity.Device, activity.DeviceName,
			models.AltitudeCorrectionNone)
		if err != nil {
			return err
		}
		activity.DeviceID = deviceID
		if store != nil {
			if err := store(activity); err != nil {
				return err
			}
		}
		return txDB.CreateActivity(activity)
	})
}

// BackfillRoutes assigns a route to the activities imported before routes existed, oldest first,
// so the first activity of a route provides its reference trace
func BackfillRoutes(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutRoute(RouteMinDistance)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	progress := newBackfillProgress(ctx, logger, "activities", len(ids), "assigning routes to activities")
	for i, id := range ids {
		// stopping: what is done stays done, the rest goes on at the next start
		if ctx.Err() != nil {
			return nil
		}
		activity, err := db.GetActivityForRoute(id)
		if err != nil {
			return err
		}
		// computed outside the transaction, saving it is useful even if the route assignment fails
		activity.SimplifiedGpsPoints, err = db.GetSimplifiedTrace(activity, SimplifyTrace)
		if err != nil {
			return err
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			txDB := &models.DB{DB: *tx}
			// another instance may have assigned it meanwhile
			if err := txDB.LockUserRoutes(activity.UserID); err != nil {
				return err
			}
			current, err := txDB.GetActivityForRoute(activity.ID)
			if err != nil {
				return err
			}
			if current.RouteID != nil {
				return nil
			}
			if err := assignRoute(txDB, activity); err != nil {
				return err
			}
			if activity.RouteID == nil {
				return nil
			}
			return txDB.SetActivityRoute(activity.ID, *activity.RouteID)
		})
		if err != nil {
			return err
		}
		progress.done(i+1, nil)
	}
	logger.Info().Str("elapsed", progress.elapsed()).Msg("routes assigned")
	return nil
}

// MergeRoutes merges the routes of a user whose traces match: the routes created when an activity didn't
// match the reference of its route, before the activities were also compared to the members of the routes.
// Each route absorbs the routes matching it directly, the ones with the most activities first: merging
// doesn't chain, a route only absorbs the routes similar to itself.
func MergeRoutes(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	if ctx.Err() != nil {
		return nil
	}
	pairs, err := db.GetRouteSports()
	if err != nil {
		return err
	}
	mergedTotal := 0
	for _, pair := range pairs {
		err = db.Transaction(func(tx *gorm.DB) error {
			txDB := &models.DB{DB: *tx}
			if err := txDB.LockUserRoutes(pair.UserID); err != nil {
				return err
			}
			routes, members, err := txDB.GetRouteCandidates(pair.UserID, pair.Sport, 0, math.MaxFloat64, RouteMembersCompared)
			if err != nil {
				return err
			}
			groups := groupRoutes(routes, members)
			for _, group := range groups {
				if err := txDB.MergeRoutes(group[0], group[1:]); err != nil {
					return err
				}
				mergedTotal += len(group) - 1
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if mergedTotal > 0 {
		logger.Info().Int("routes", mergedTotal).Msg("similar routes merged")
	}
	return nil
}

// groupRoutes returns the groups of routes to merge, each one starting with the route to keep: the one with
// the most activities, the oldest when equal
func groupRoutes(routes []models.Route, members []models.RouteReference) [][]models.Route {
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].ActivityCount != routes[j].ActivityCount {
			return routes[i].ActivityCount > routes[j].ActivityCount
		}
		return routes[i].CreatedAt.Before(routes[j].CreatedAt)
	})
	references := referencesByRoute(routes, members)
	absorbed := make([]bool, len(routes))
	var groups [][]models.Route
	for i := range routes {
		if absorbed[i] {
			continue
		}
		group := []models.Route{routes[i]}
		for j := i + 1; j < len(routes); j++ {
			if absorbed[j] {
				continue
			}
			if bestSimilarity(references[routes[i].ID], references[routes[j].ID]) >= similarMinCoverage {
				absorbed[j] = true
				group = append(group, routes[j])
			}
		}
		if len(group) > 1 {
			groups = append(groups, group)
		}
	}
	return groups
}

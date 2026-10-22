package activityService

import (
	"MySportWeb/internal/pkg/models"

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

// MatchRoute returns the route the activity follows with the best similarity, nil if there is none
func MatchRoute(activity models.Activity, routes []models.Route) *models.Route {
	var best *models.Route
	bestSimilarity := 0.0
	for i, route := range routes {
		if route.Sport != activity.Sport || !DistancesMatch(activity.Distance, route.Distance) ||
			!EndpointsMatch(activity.StartPosition, activity.EndPosition, route.StartPosition, route.EndPosition) {
			continue
		}
		similarity := TraceSimilarity(activity.SimplifiedGpsPoints, route.SimplifiedGpsPoints)
		if similarity >= similarMinCoverage && similarity > bestSimilarity {
			best = &routes[i]
			bestSimilarity = similarity
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
	routes, err := db.GetRouteCandidates(activity.UserID, activity.Sport,
		activity.Distance*(1-SimilarDistanceTolerance), activity.Distance*(1+SimilarDistanceTolerance))
	if err != nil {
		return err
	}
	route := MatchRoute(*activity, routes)
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
		activity.IsCommute = true
		if activity.Co2 == 0 {
			activity.Co2 = Commute(*activity)
		}
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
func BackfillRoutes(db *models.DB, logger zerolog.Logger) error {
	ids, err := db.GetActivitiesWithoutRoute(RouteMinDistance)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	logger.Info().Int("activities", len(ids)).Msg("assigning routes to activities")
	for _, id := range ids {
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
	}
	logger.Info().Msg("routes assigned")
	return nil
}

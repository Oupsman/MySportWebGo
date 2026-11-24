package models

import (
	"MySportWeb/internal/pkg/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

// Route groups the activities of a user following the same path
type Route struct {
	ID            uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	UserID        uint      `gorm:"index" json:"user_id"`
	Name          string    `json:"name"`
	Sport         string    `json:"sport"`
	Distance      float64   `json:"distance"`
	ActivityCount int       `json:"activity_count"`
	// the activities of a commute route are commutes, including the ones imported later
	IsCommute bool `json:"is_commute"`
	// reference trace, taken from the first activity of the route
	StartPosition       *types.GpsPoint     `json:"-"`
	EndPosition         *types.GpsPoint     `json:"-"`
	SimplifiedGpsPoints types.GpsPointArray `json:"-"`
}

// LockUserRoutes serializes route assignment for a user until the end of the transaction,
// so two similar activities imported at the same time don't create two routes
func (db *DB) LockUserRoutes(userID uint) error {
	return db.Exec("SELECT pg_advisory_xact_lock(?::int, ?::int)", advisoryLockRoutes, userID).Error
}

const advisoryLockRoutes = 1

// RouteReference is a trace an activity is compared to when looking for its route: one of the activities of
// the route
type RouteReference struct {
	RouteID             uuid.UUID
	Distance            float64
	StartPosition       *types.GpsPoint
	EndPosition         *types.GpsPoint
	SimplifiedGpsPoints types.GpsPointArray
}

// GetRouteCandidates returns the user's routes of the sport whose reference, or one of the activities, has a
// distance between minDistance and maxDistance, and the traces of these activities: at most perRoute per
// route, the most recent ones
func (db *DB) GetRouteCandidates(userID uint, sport string, minDistance, maxDistance float64, perRoute int) ([]Route, []RouteReference, error) {
	var members []RouteReference
	err := db.Raw(`SELECT route_id, distance, start_position, end_position, simplified_gps_points FROM (
			SELECT route_id, distance, start_position, end_position, simplified_gps_points,
				ROW_NUMBER() OVER (PARTITION BY route_id ORDER BY date DESC) AS n
			FROM activities
			WHERE user_id = ? AND sport = ? AND route_id IS NOT NULL AND distance BETWEEN ? AND ?
				AND start_position IS NOT NULL AND end_position IS NOT NULL AND simplified_gps_points IS NOT NULL
		) m WHERE n <= ?`, userID, sport, minDistance, maxDistance, perRoute).Scan(&members).Error
	if err != nil {
		return nil, nil, err
	}
	routeIDs := []uuid.UUID{}
	for _, member := range members {
		routeIDs = append(routeIDs, member.RouteID)
	}
	var routes []Route
	err = db.Where("user_id = ? AND sport = ? AND (distance BETWEEN ? AND ? OR id IN ?)",
		userID, sport, minDistance, maxDistance, routeIDs).Find(&routes).Error
	if err != nil {
		return nil, nil, err
	}
	return routes, members, nil
}

// GetRouteSports returns the users and sports having routes
func (db *DB) GetRouteSports() ([]Route, error) {
	var pairs []Route
	err := db.Model(&Route{}).Distinct("user_id", "sport").Order("user_id, sport").Find(&pairs).Error
	return pairs, err
}

// MergeRoutes moves the activities of the merged routes to the kept one, and deletes them. The kept route
// is a commute when one of them was, its activities are then all commutes, see SetRouteCommute.
func (db *DB) MergeRoutes(kept Route, merged []Route) error {
	ids := make([]uuid.UUID, len(merged))
	isCommute, allCommutes := kept.IsCommute, kept.IsCommute
	for i, route := range merged {
		ids[i] = route.ID
		isCommute = isCommute || route.IsCommute
		allCommutes = allCommutes && route.IsCommute
	}
	err := db.Model(&Activity{}).Where("route_id IN ?", ids).Update("route_id", kept.ID).Error
	if err != nil {
		return err
	}
	err = db.Where("id IN ?", ids).Delete(&Route{}).Error
	if err != nil {
		return err
	}
	// counted again rather than summed: fixes a count gone wrong
	err = db.Model(&Route{}).Where("id = ?", kept.ID).
		Update("activity_count", db.Model(&Activity{}).Select("count(*)").Where("route_id = ?", kept.ID)).Error
	if err != nil {
		return err
	}
	if isCommute && !allCommutes {
		_, err = db.SetRouteCommute(kept.ID, true)
	}
	return err
}

func (db *DB) GetRoute(id uuid.UUID) (*Route, error) {
	var route Route
	err := db.First(&route, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &route, nil
}

// SetRouteCommute flags a route and all its activities as commutes or not. The commutes without a saved
// CO2 (the ones that aren't rides) get one, the rides get the user's commute equipment when there is one.
// It returns the number of activities of the route.
func (db *DB) SetRouteCommute(routeID uuid.UUID, isCommute bool) (int64, error) {
	var count int64
	err := db.Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Route{}).Where("id = ?", routeID).Update("is_commute", isCommute).Error
		if err != nil {
			return err
		}
		updates := map[string]interface{}{"is_commute": isCommute}
		if isCommute {
			updates["co2"] = gorm.Expr("CASE WHEN co2 > 0 THEN co2 ELSE distance * ? END", Co2PerMeter)
		}
		result := tx.Model(&Activity{}).Where("route_id = ?", routeID).Updates(updates)
		count = result.RowsAffected
		if result.Error != nil || !isCommute {
			return result.Error
		}
		var bikes []Equipments
		err = tx.Where("is_commute AND user_id = (SELECT user_id FROM routes WHERE id = ?)", routeID).
			Limit(1).Find(&bikes).Error
		if err != nil || len(bikes) == 0 {
			return err
		}
		return tx.Model(&Activity{}).Where("route_id = ? AND sport = ?", routeID, CommuteSport).
			Update("equipment_id", bikes[0].ID).Error
	})
	return count, err
}

// SetRouteEndpoints replaces the reference start and end of a route
func (db *DB) SetRouteEndpoints(routeID uuid.UUID, start, end *types.GpsPoint) error {
	return db.Model(&Route{}).Where("id = ?", routeID).
		Updates(map[string]interface{}{"start_position": start, "end_position": end}).Error
}

func (db *DB) CreateRoute(route *Route) error {
	return db.Create(route).Error
}

func (db *DB) IncrementRouteCount(routeID uuid.UUID) error {
	return db.Model(&Route{}).Where("id = ?", routeID).
		Update("activity_count", gorm.Expr("activity_count + 1")).Error
}

// SetActivityRoute links an already saved activity to a route
func (db *DB) SetActivityRoute(activityID, routeID uuid.UUID) error {
	return db.Model(&Activity{}).Where("id = ?", activityID).Update("route_id", routeID).Error
}

// GetActivitiesByRoute returns the activities of a route, except excludeID, most recent first
func (db *DB) GetActivitiesByRoute(routeID, excludeID uuid.UUID) ([]types.RouteActivity, error) {
	activities := []types.RouteActivity{}
	// only the listed columns: a full row holds every record of the FIT file
	err := db.Table("activities").Select("id", "title", "date", "distance", "duration", "avg_speed", "moving_time", "avg_moving_speed").
		Where("route_id = ? AND id <> ?", routeID, excludeID).
		Order("date desc").Scan(&activities).Error
	if err != nil {
		return nil, err
	}
	return activities, nil
}

// GetActivitiesWithoutRoute returns, oldest first, the ids of the activities that may get a route
func (db *DB) GetActivitiesWithoutRoute(minDistance float64) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).
		Where("route_id IS NULL AND start_position IS NOT NULL AND distance >= ?", minDistance).
		Order("date asc").Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// GetActivityForRoute returns the fields of an activity needed to assign it a route
func (db *DB) GetActivityForRoute(id uuid.UUID) (*Activity, error) {
	var activity Activity
	err := db.Select("id", "user_id", "title", "sport", "distance", "start_position", "end_position",
		"simplified_gps_points", "route_id").First(&activity, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &activity, nil
}

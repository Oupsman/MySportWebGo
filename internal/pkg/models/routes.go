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

// GetRouteCandidates returns the user's routes of the same sport with a distance between minDistance and maxDistance
func (db *DB) GetRouteCandidates(userID uint, sport string, minDistance, maxDistance float64) ([]Route, error) {
	var routes []Route
	err := db.Where("user_id = ? AND sport = ? AND distance BETWEEN ? AND ?", userID, sport, minDistance, maxDistance).
		Find(&routes).Error
	if err != nil {
		return nil, err
	}
	return routes, nil
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
// CO2 (the ones that aren't rides) get one. It returns the number of activities of the route.
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
		return result.Error
	})
	return count, err
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
	err := db.Table("activities").Select("id", "title", "date", "distance", "duration", "avg_speed").
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

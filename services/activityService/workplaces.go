package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"

	"github.com/google/uuid"
)

// the diameters a workplace can have, in meters
const (
	WorkplaceMinDiameter = 20.0
	WorkplaceMaxDiameter = 5000.0
)

// InWorkplace tells if a position is in the circle of a workplace
func InWorkplace(p *types.GpsPoint, w models.Workplace) bool {
	if p == nil || (p.Lat == 0 && p.Lon == 0) {
		return false
	}
	return utils.Haversine(*p, types.GpsPoint{Lat: w.Lat, Lon: w.Lon})*1000 <= w.Diameter/2
}

// startsOrEndsAt tells if an activity starts or ends in one of the workplaces
func startsOrEndsAt(start, end *types.GpsPoint, workplaces []models.Workplace) bool {
	for _, w := range workplaces {
		if InWorkplace(start, w) || InWorkplace(end, w) {
			return true
		}
	}
	return false
}

// markCommute makes an activity being imported a commute: the CO2 it saved, and the commute bike instead of
// the default equipment for a ride recorded since its purchase
func markCommute(db *models.DB, activity *models.Activity) error {
	activity.IsCommute = true
	if activity.Co2 == 0 {
		activity.Co2 = Commute(*activity)
	}
	if activity.Sport != models.CommuteSport {
		return nil
	}
	bike, err := db.GetCommuteEquipment(activity.UserID)
	if err != nil || bike == nil || !bike.OwnedOn(activity.Date) {
		return err
	}
	activity.Equipment = *bike
	activity.EquipmentID = &bike.ID
	return nil
}

// commuteFromWorkplaces makes an activity being imported a commute when it starts or ends at a workplace of
// its user
func commuteFromWorkplaces(db *models.DB, activity *models.Activity) error {
	if activity.IsCommute {
		return nil
	}
	workplaces, err := db.GetWorkplaces(activity.UserID)
	if err != nil || !startsOrEndsAt(activity.StartPosition, activity.EndPosition, workplaces) {
		return err
	}
	return markCommute(db, activity)
}

// ApplyWorkplace makes the activities of a user starting or ending at a workplace commutes, it returns how
// many became commutes
func ApplyWorkplace(db *models.DB, userID uint, workplace models.Workplace) (int, error) {
	candidates, err := db.CommuteCandidates(userID)
	if err != nil {
		return 0, err
	}
	workplaces := []models.Workplace{workplace}
	var ids []uuid.UUID
	for _, c := range candidates {
		if startsOrEndsAt(c.StartPosition, c.EndPosition, workplaces) {
			ids = append(ids, c.ID)
		}
	}
	return len(ids), db.MarkCommutes(userID, ids)
}

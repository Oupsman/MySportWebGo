package activityService

import (
	"MySportWeb/internal/pkg/models"

	"github.com/google/uuid"
)

// Commute simply returns the saved CO2 comparing to a commute by car
func Commute(activity models.Activity) float32 {
	return float32(activity.Distance * models.Co2PerMeter)
}

// EquipmentOnUpdate returns the equipment to save when the user edits an activity: a ride becoming a
// commute gets the commute equipment, unless the user chooses another equipment in the same edit.
func EquipmentOnUpdate(db *models.DB, activity models.Activity, isCommute bool, requested *uuid.UUID) (*uuid.UUID, error) {
	unchanged := (requested == nil) == (activity.EquipmentID == nil) &&
		(requested == nil || *requested == *activity.EquipmentID)
	if !isCommute || activity.IsCommute || !unchanged || activity.Sport != models.CommuteSport {
		return requested, nil
	}
	bike, err := db.GetCommuteEquipment(activity.UserID)
	if err != nil || bike == nil {
		return requested, err
	}
	return &bike.ID, nil
}

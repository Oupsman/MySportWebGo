package activityService

import (
	"MySportWeb/internal/pkg/models"
	"slices"
	"time"

	"github.com/muktihari/fit/profile/typedef"
)

// ImportWeight returns the weight of a user at the date of an activity being imported, with the default
// equipment of its sport (none without one): the equipment AssignDefaultEquipment gives it
func ImportWeight(db *models.DB, userID uint) WeightFunc {
	return func(date time.Time, sport string) (float64, error) {
		equipment, err := db.DefaultEquipment(userID, sport)
		if err != nil {
			return 0, err
		}
		weight := 0
		if equipment != nil {
			weight = equipment.Weight
		}
		return UserWeight(db, userID, weight)(date, sport)
	}
}

// AssignDefaultEquipment gives an analysed activity the default equipment of its sport, when its user has
// one. A commute ride gets the commute bike instead, when it is saved (assignRoute).
func AssignDefaultEquipment(db *models.DB, activity *models.Activity) error {
	equipment, err := db.DefaultEquipment(activity.UserID, activity.Sport)
	if err != nil || equipment == nil {
		return err
	}
	activity.Equipment = *equipment
	activity.EquipmentID = &equipment.ID
	return nil
}

// IsSportName tells whether a name is one SportName returns: a sport of the FIT profile
func IsSportName(name string) bool {
	for sport := typedef.Sport(0); sport < typedef.SportAll; sport++ {
		if SportName(sport) == name {
			return true
		}
	}
	return false
}

// SportNames returns the names of the sports of the FIT profile, sorted
func SportNames() []string {
	var names []string
	for sport := typedef.Sport(0); sport < typedef.SportAll; sport++ {
		if name := SportName(sport); name != typedef.SportGeneric.String() || sport == typedef.SportGeneric {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

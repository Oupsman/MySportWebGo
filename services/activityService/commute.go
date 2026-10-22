package activityService

import "MySportWeb/internal/pkg/models"

// Commute simply returns the saved CO2 comparing to a commute by car
func Commute(activity models.Activity) float32 {
	return float32(activity.Distance * models.Co2PerMeter)
}

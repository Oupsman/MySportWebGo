package activityService

import "MySportWeb/internal/pkg/models"

func Commute(activity models.Activity) float32 {
	return float32(activity.Distance * 0.2217)
}

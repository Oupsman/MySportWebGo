package activityService

import "MySportWeb/internal/pkg/models"

func Commute(activity models.Activity) uint32 {
	return uint32(activity.Distance * 110)
}

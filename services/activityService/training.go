package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/utils"
)

func CalculateCalories(activity models.Activity) float64 {
	return (float64(activity.Duration) * 60 * GetMet(activity.Sport) * 3.5 * float64(activity.TotalWeight)) / 200
}

func GetMet(sport string) float64 {
	switch sport {
	case "cycling":
		return 9.5
	case "running":
		return 10
	case "hiking":
		return 6
	}
	return 6
}

func CalculateTrainingScore(activity models.Activity) uint32 {
	heartNorm := utils.NormalizedAvg(activity.Hearts)
	return uint32(float64(activity.Duration)/600+heartNorm/float64(activity.User.GetMaxHR(activity.Date))) * 10
}

func CalculateTrainingEffect(activity models.Activity) float64 {
	powerNorm := utils.NormalizedAvg(activity.Powers)
	return powerNorm / activity.User.EstimateFTP(activity.Date)
}

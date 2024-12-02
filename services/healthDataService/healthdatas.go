package healthDataService

import (
	"MySportWeb/internal/pkg/models"
	"strconv"
	"time"
)

func ParseHealthDatas(row []string, user models.Users) (models.HealthData, error) {

	var healthData = models.HealthData{}
	var err error
	healthData.User = user

	healthData.Date, err = time.Parse("2006-01-02 15:04", row[0])
	if err != nil {
		return models.HealthData{}, err
	}
	healthData.Weight, err = strconv.ParseFloat(row[1], 64)
	if err != nil {
		return models.HealthData{}, err
	}
	healthData.Fat, err = strconv.ParseFloat(row[2], 64)
	if err != nil {
		return models.HealthData{}, err
	}
	healthData.Bone, err = strconv.ParseFloat(row[3], 64)
	if err != nil {
		return models.HealthData{}, err
	}
	healthData.Muscle, err = strconv.ParseFloat(row[4], 64)
	if err != nil {
		return models.HealthData{}, err
	}
	healthData.BodyWater, err = strconv.ParseFloat(row[5], 64)
	if err != nil {
		return models.HealthData{}, err
	}
	return healthData, nil
}

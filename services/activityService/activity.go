package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/proto"
	"math"
	"os"
	"time"
)

// var decoderPool = sync.Pool{New: func() any { return decoder.New(nil) }}

func SumAnalyze(filePath string, user models.Users, equipment models.Equipments) (models.Activity, error) {
	var activity models.Activity
	f, err := os.Open(filePath)
	if err != nil {
		return models.Activity{}, err
	}
	defer f.Close()

	dec := decoder.New(f)

	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			panic(err)
		}
		activity, err = DecodeFit(fit, user, equipment)
		if err != nil {
			return models.Activity{}, err
		}
	}

	return activity, nil
}

func DecodeFit(fit *proto.FIT, user models.Users, equipment models.Equipments) (models.Activity, error) {
	var activity models.Activity
	var err error
	fitActivity := filedef.NewActivity(fit.Messages...)

	activity.User = user
	activity.Equipment = equipment
	activity.Sport = fitActivity.Sessions[0].Sport.String()
	activity.Date = fitActivity.Sessions[0].StartTime
	activity.Duration = fitActivity.Sessions[0].TotalTimerTime / 1000
	if fitActivity.Sessions[0].TotalDistance > 0 {
		activity.Distance = fitActivity.Sessions[0].TotalDistanceScaled()
	} else {
		activity.Distance = 0
	}
	activity.Calories = fitActivity.Sessions[0].TotalCalories
	activity.AvgSpeed = fitActivity.Sessions[0].AvgSpeedScaled()
	activity.AvgRPM = fitActivity.Sessions[0].AvgCadence
	activity.AvgHR = fitActivity.Sessions[0].AvgHeartRate
	if fitActivity.Sessions[0].AvgPower != math.MaxUint16 {
		activity.AvgPower = fitActivity.Sessions[0].AvgPower
	}
	if fitActivity.Sessions[0].AvgPower > 0 {
		activity.AvgPower = fitActivity.Sessions[0].AvgPower
	}
	activity.TotalAscent = fitActivity.Sessions[0].TotalAscent
	activity.TotalDescent = fitActivity.Sessions[0].TotalDescent
	activity.MaxSpeed = fitActivity.Sessions[0].MaxSpeedScaled()

	// TODO : one day, I'll get the weight of the user and its equipment
	activity.TotalWeight = 110

	activity, err = AnalyzeRecords(fitActivity, activity)
	if len(fitActivity.Lengths) > 0 {
		activity.Lengths = AnalyzeLengths(fitActivity)
	}
	if err != nil {
		return models.Activity{}, err
	}
	return activity, nil
}

func AnalyzeRecords(fitActivity *filedef.Activity, activity models.Activity) (models.Activity, error) {
	var startmeters, stopmeters, meters = 0.0, 0.0, 0.0
	var startaltitude, stopaltitude = 0.0, 0.0
	var avgSpeedT = 0.0
	var km = 0.0
	var hasPower = false
	var tsstartkm time.Time
	var tsstartdist float64
	var counter = 0
	var recordsCount = len(fitActivity.Records)
	var altitudes, filtered []float64

	distance := activity.Distance
	weight := activity.TotalWeight

	if recordsCount > 0 {
		tsstartkm = fitActivity.Records[0].Timestamp
		tsstartdist = fitActivity.Records[0].DistanceScaled()
	}
	if distance == 0 {
		distance = 1
	}
	if fitActivity.Sessions[0].AvgPower != math.MaxUint16 {
		hasPower = true
	}
	for km < distance && counter < recordsCount-1 {
		stopkm := km + 1.0
		for km < stopkm && counter < recordsCount-1 {
			var deltaDistance, deltaAltitude float64
			stopmeters = meters + 30
			startmeters = fitActivity.Records[counter].DistanceScaled()
			startaltitude = fitActivity.Records[counter].EnhancedAltitudeScaled()
			record := fitActivity.Records[counter]
			for meters < stopmeters && counter < recordsCount-1 {
				if counter < recordsCount-1 {
					if record.HeartRate != math.MaxUint8 {
						activity.Hearts = append(activity.Hearts, record.HeartRate)
					}
					if record.Temperature != math.MaxInt8 {
						activity.Temperatures = append(activity.Temperatures, record.Temperature)
					}

					if record.PositionLat != math.MaxInt32 && record.PositionLong != math.MaxInt32 && record.SpeedScaled() != math.MaxFloat64 {
						activity.Lats = append(activity.Lats, utils.SemiCircleToDegres(record.PositionLat))
						activity.Lons = append(activity.Lons, utils.SemiCircleToDegres(record.PositionLong))
						activity.GpsPoints = append(activity.GpsPoints, types.GpsPoint{
							Lat: utils.SemiCircleToDegres(record.PositionLat),
							Lon: utils.SemiCircleToDegres(record.PositionLong),
						})
						activity.Speeds = append(activity.Speeds, record.SpeedScaled())
						activity.Distances = append(activity.Distances, record.DistanceScaled())
						// not directly writing in the activity struct here, since we may want to apply a filter on the array
						altitudes = append(altitudes, record.EnhancedAltitudeScaled())
						km = record.DistanceScaled() / 1000
						meters = record.DistanceScaled()

						// if the user has a security distance, we add the points to the public gps points
						if float64(activity.User.SecurityDistance) > record.DistanceScaled() && fitActivity.Sessions[0].TotalDistanceScaled()-float64(activity.User.SecurityDistance) < record.DistanceScaled() {
							activity.PublicGpsPoints = append(activity.PublicGpsPoints, types.GpsPoint{
								Lat: utils.SemiCircleToDegres(record.PositionLat),
								Lon: utils.SemiCircleToDegres(record.PositionLong),
							})
						}

					}
					if hasPower && activity.Sport == "cycling" {
						if record.Power != math.MaxUint16 {
							activity.Powers = append(activity.Powers, record.Power)
						}
					}
					if record.SpeedScaled() != math.MaxFloat64 && record.SpeedScaled() > activity.MaxSpeed {
						activity.MaxSpeed = record.SpeedScaled()
						activity.MaxSpeedPosition = types.GpsPoint{
							Lat: utils.SemiCircleToDegres(record.PositionLat),
							Lon: utils.SemiCircleToDegres(record.PositionLong),
						}
					}
					if record.Cadence != math.MaxUint8 {
						activity.Cadences = append(activity.Cadences, record.Cadence)
					}

				}
				counter++

			}
			stopaltitude = record.EnhancedAltitudeScaled()
			if activity.Sport == "cycling" && !hasPower {
				var slope float64
				deltaDistance = stopmeters - startmeters
				deltaAltitude = stopaltitude - startaltitude
				speed := record.SpeedScaled()
				if deltaDistance > 10 && deltaDistance < 80 {
					slope = deltaAltitude / deltaDistance
				} else {
					slope = 0
				}
				power := (utils.GravityFactor(float64(weight), slope) +
					utils.RollingResistance(float64(weight), slope) +
					utils.AerodynamicDrag(stopaltitude)) * speed / (1 - 0.045)
				activity.Powers = append(activity.Powers, uint16(power))

			}
		}
		if len(activity.Powers) > 0 {
			activity.AvgPower = uint16(utils.Avg(activity.Powers))

		}
		tsstopdist := fitActivity.Records[counter-1].DistanceScaled()
		tsstopkm := fitActivity.Records[counter-1].Timestamp
		delta := tsstopkm.Sub(tsstartkm)
		dist := tsstopdist - tsstartdist
		if delta.Seconds() > 0 {
			avgSpeedT = dist / delta.Seconds()
		}
		tsstartkm = tsstopkm
		tsstartdist = tsstopdist
		activity.Means = append(activity.Means, avgSpeedT)

	}
	counter = 0

	if len(altitudes) > 200 {
		filtered = utils.SavitzkyGolay(altitudes, 47, 3)
	} else {
		filtered = altitudes
	}

	if len(altitudes) > 2 {
		ComputeElevation(&activity, filtered)
	}

	activity.GpsCenter = &types.GpsPoint{
		Lat: utils.Avg(activity.Lats),
		Lon: utils.Avg(activity.Lons),
	}
	activity.GpsBounds = []types.GpsPoint{
		{Lat: utils.Min(activity.Lats), Lon: utils.Min(activity.Lons)},
		{Lat: utils.Max(activity.Lats), Lon: utils.Max(activity.Lons)},
	}
	return activity, nil
}

func ComputeElevation(activity *models.Activity, filtered []float64) {
	counter := 0
	recordsCount := len(filtered)
	for counter < recordsCount-2 {
		var value float64
		if counter+1 < len(filtered) {
			value = filtered[counter+1] - filtered[counter]
		} else {
			value = 0
		}

		if math.Abs(value) <= 5 {
			if value >= 0 {
				activity.PositiveElevation += value
			} else {
				activity.NegativeElevation += value
			}
		}
		counter++
	}
}

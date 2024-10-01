package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"MySportWeb/internal/pkg/vars"
	"errors"
	"fmt"
	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/proto"
	"github.com/pconstantinou/savitzkygolay"
	"math"
	"os"
	"strings"
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
	// Some FIT files downloaded from Garmin Connect seems to be corrupted with wrong checksum
	// So ignoring the checksum
	// Maybe add an option to correct the FIT file.
	dec := decoder.New(f, decoder.WithIgnoreChecksum())

	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			return models.Activity{}, err
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
	activity.EngineVersion = vars.Engine_version
	activity.User = user
	activity.Equipment = equipment
	activity.Sport = fitActivity.Sessions[0].Sport.String()
	if strings.Contains(activity.Sport, "Invalid") {
		return models.Activity{}, errors.New("Invalid sport")
	}
	activity.Date = fitActivity.Sessions[0].StartTime
	activity.Duration = fitActivity.Sessions[0].TotalTimerTime / 1000
	if !math.IsNaN(fitActivity.Sessions[0].TotalDistanceScaled()) {
		activity.Distance = fitActivity.Sessions[0].TotalDistanceScaled()
	} else {
		activity.Distance = 0
	}
	if fitActivity.Sessions[0].TotalCalories != math.MaxUint16 {
		activity.Calories = fitActivity.Sessions[0].TotalCalories
	}
	if !math.IsNaN(fitActivity.Sessions[0].EnhancedAvgSpeedScaled()) {
		activity.AvgSpeed = fitActivity.Sessions[0].EnhancedAvgSpeedScaled()
	} else if !math.IsNaN(fitActivity.Sessions[0].AvgSpeedScaled()) {
		activity.AvgSpeed = fitActivity.Sessions[0].AvgSpeedScaled()
	} else {
		activity.AvgSpeed = 0
	}
	if !math.IsNaN(fitActivity.Sessions[0].EnhancedMaxSpeedScaled()) {
		activity.MaxSpeed = fitActivity.Sessions[0].EnhancedMaxSpeedScaled()
	} else if !math.IsNaN(fitActivity.Sessions[0].MaxSpeedScaled()) {
		activity.MaxSpeed = fitActivity.Sessions[0].MaxSpeedScaled()
	} else {
		activity.MaxSpeed = 0
	}
	if fitActivity.Sessions[0].AvgCadence != math.MaxUint8 {
		activity.AvgRPM = fitActivity.Sessions[0].AvgCadence
	}
	if fitActivity.Sessions[0].AvgHeartRate != math.MaxUint8 {
		activity.AvgHR = fitActivity.Sessions[0].AvgHeartRate
	}
	if fitActivity.Sessions[0].AvgPower != math.MaxUint16 {
		activity.AvgPower = fitActivity.Sessions[0].AvgPower
	}
	if fitActivity.Sessions[0].AvgPower > 0 && fitActivity.Sessions[0].AvgPower != math.MaxUint16 {
		activity.AvgPower = fitActivity.Sessions[0].AvgPower
	}
	activity.TotalAscent = fitActivity.Sessions[0].TotalAscent
	activity.TotalDescent = fitActivity.Sessions[0].TotalDescent

	// TODO : one day, I'll get the weight of the user and its equipment
	activity.TotalWeight = 110

	if activity.Date.Hour() > 0 && activity.Date.Hour() < 12 {
		activity.Title = "Morning " + activity.Sport
	} else if activity.Date.Hour() >= 12 && activity.Date.Hour() < 14 {
		activity.Title = "Lunch " + activity.Sport
	} else if activity.Date.Hour() >= 14 && activity.Date.Hour() < 18 {
		activity.Title = "Afternoon " + activity.Sport
	} else if activity.Date.Hour() >= 18 && activity.Date.Hour() < 22 {
		activity.Title = "Evening " + activity.Sport
	} else {
		activity.Title = "Night " + activity.Sport
	}

	activity, err = AnalyzeRecords(fitActivity, activity)
	if err != nil {
		return models.Activity{}, err
	}
	if len(fitActivity.Lengths) > 0 {
		activity.Lengths = AnalyzeLengths(fitActivity)
	}
	if activity.Sport == "cycling" {
		activity.Co2 = Commute(activity)
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
	var securityDistance = float64(activity.User.SecurityDistance) / 1000
	var startPosition, endPosition types.GpsPoint

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

	// Find first valid GPS points in records
	if distance > 0 {
		for counter < recordsCount && (fitActivity.Records[counter].PositionLat == math.MaxInt32 ||
			fitActivity.Records[counter].PositionLong == math.MaxInt32) {
			counter++
		}
		if counter < recordsCount {
			startPosition = types.GpsPoint{
				Lat: utils.SemiCircleToDegres(fitActivity.Records[counter].PositionLat),
				Lon: utils.SemiCircleToDegres(fitActivity.Records[counter].PositionLong),
			}
			endPosition = types.GpsPoint{
				Lat: utils.SemiCircleToDegres(fitActivity.Records[recordsCount-1].PositionLat),
				Lon: utils.SemiCircleToDegres(fitActivity.Records[recordsCount-1].PositionLong),
			}
			if utils.Haversine(startPosition, endPosition) < securityDistance {
				// Activity is a loop so we double the security distance
				securityDistance = 2 * securityDistance
			}
		}
	}
	counter = 0
	for km < distance && counter < recordsCount-1 {
		stopkm := km + 1.0
		for km < stopkm && counter < recordsCount-1 {
			var deltaDistance, deltaAltitude float64
			stopmeters = meters + 10
			startmeters = fitActivity.Records[counter].DistanceScaled()
			startaltitude = fitActivity.Records[counter].EnhancedAltitudeScaled()
			//record := fitActivity.Records[counter]
			var record *mesgdef.Record
			for meters < stopmeters && counter < recordsCount-1 {
				if counter < recordsCount-1 {
					record = fitActivity.Records[counter]
					activity.TimeStamps = append(activity.TimeStamps, fmt.Sprintf("%f", record.Timestamp.Sub(fitActivity.Sessions[0].StartTime).Seconds()))
					if record.HeartRate != math.MaxUint8 {
						activity.Hearts = append(activity.Hearts, uint16(record.HeartRate))
					}
					if record.Temperature != math.MaxInt8 {
						activity.Temperatures = append(activity.Temperatures, int16(record.Temperature))
					}
					if !math.IsNaN(record.EnhancedSpeedScaled()) && record.EnhancedSpeedScaled() != 65.535 {
						activity.Speeds = append(activity.Speeds, record.EnhancedSpeedScaled())
					} else if !math.IsNaN(record.SpeedScaled()) {
						activity.Speeds = append(activity.Speeds, record.SpeedScaled())
					} else {
						activity.Speeds = append(activity.Speeds, 0)
					}
					if !math.IsNaN(record.DistanceScaled()) {
						activity.Distances = append(activity.Distances, record.DistanceScaled())
					} else {
						activity.Distances = append(activity.Distances, 0)
					}
					// not directly writing in the activity struct here, since we may want to apply a filter on the array
					if !math.IsNaN(record.EnhancedAltitudeScaled()) && !math.IsNaN(record.DistanceScaled()) {
						altitudes = append(altitudes, record.EnhancedAltitudeScaled())
						km = record.DistanceScaled() / 1000
						meters = record.DistanceScaled()
					} else if !math.IsNaN(record.AltitudeScaled()) && !math.IsNaN(record.DistanceScaled()) {
						altitudes = append(altitudes, record.AltitudeScaled())
					} else {
						altitudes = append(altitudes, 0)
					}
					if record.PositionLat != math.MaxInt32 && record.PositionLong != math.MaxInt32 {
						activity.Lats = append(activity.Lats, utils.SemiCircleToDegres(record.PositionLat))
						activity.Lons = append(activity.Lons, utils.SemiCircleToDegres(record.PositionLong))
						currentPoint := types.GpsPoint{
							Lat: utils.SemiCircleToDegres(record.PositionLat),
							Lon: utils.SemiCircleToDegres(record.PositionLong),
						}
						activity.GpsPoints = append(activity.GpsPoints, currentPoint)

						// if the user has a security distance, we add the points to the public gps points
						distanceFromStart := utils.Haversine(startPosition, currentPoint)
						distanceFromEnd := utils.Haversine(endPosition, currentPoint)
						if distanceFromStart > securityDistance &&
							distanceFromEnd > securityDistance {
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
					if !math.IsNaN(record.SpeedScaled()) && record.SpeedScaled() > activity.MaxSpeed {
						activity.MaxSpeed = record.SpeedScaled()
						activity.MaxSpeedPosition = types.GpsPoint{
							Lat: utils.SemiCircleToDegres(record.PositionLat),
							Lon: utils.SemiCircleToDegres(record.PositionLong),
						}
					}
					if record.Cadence != math.MaxUint8 {
						activity.Cadences = append(activity.Cadences, uint16(record.Cadence))
					}
				}
				counter++
			}
			stopaltitude = record.EnhancedAltitudeScaled()
			if activity.Sport == "cycling" && !hasPower {
				var slope float64
				deltaDistance = stopmeters - startmeters
				deltaAltitude = stopaltitude - startaltitude
				var speed float64
				if !math.IsNaN(record.EnhancedSpeedScaled()) {
					speed = record.EnhancedSpeedScaled()
				} else {
					speed = record.SpeedScaled()
				}
				if deltaDistance > 5 && deltaDistance < 20 {
					slope = deltaAltitude / deltaDistance
				} else {
					slope = 0
				}
				power := (utils.GravityFactor(float64(weight), slope) +
					utils.RollingResistance(float64(weight), slope) +
					utils.AerodynamicDrag(stopaltitude)) * speed / (1 - 0.045)
				if power > 0 {
					activity.Powers = append(activity.Powers, uint16(power))
				} else {
					activity.Powers = append(activity.Powers, 0)
				}
				activity.PowerTS = append(activity.PowerTS, fmt.Sprintf("%f", fitActivity.Records[counter].Timestamp.Sub(fitActivity.Sessions[0].StartTime).Seconds()))

			}
		}
		if len(activity.Powers) > 0 {
			avgPower := utils.NormalizedAvgPositive(activity.Powers)
			activity.AvgPower = uint16(avgPower)

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
		if !math.IsNaN(avgSpeedT) {
			activity.Means = append(activity.Means, avgSpeedT)
		}

	}
	counter = 0

	if len(altitudes) > 200 {
		filter, err := savitzkygolay.NewFilterWindow(47)
		if err != nil {
			return models.Activity{}, err
		}
		ys := make([]float64, len(altitudes))
		for i, _ := range altitudes {
			ys[i] = float64(i)
		}
		filtered, err = filter.Process(altitudes, ys)
		if err != nil {
			return models.Activity{}, err
		}
	} else {
		filtered = altitudes
	}
	activity.Altitudes = filtered
	if len(altitudes) > 2 {
		ComputeElevation(&activity, filtered)
	}
	if len(activity.GpsPoints) > 2 {
		activity.StartPosition = &types.GpsPoint{
			Lat: utils.SemiCircleToDegres(fitActivity.Records[0].PositionLat),
			Lon: utils.SemiCircleToDegres(fitActivity.Records[0].PositionLong),
		}
		activity.EndPosition = &types.GpsPoint{
			Lat: utils.SemiCircleToDegres(fitActivity.Records[len(fitActivity.Records)-1].PositionLat),
			Lon: utils.SemiCircleToDegres(fitActivity.Records[len(fitActivity.Records)-1].PositionLong),
		}
		activity.GpsCenter = &types.GpsPoint{
			Lat: utils.Avg(activity.Lats),
			Lon: utils.Avg(activity.Lons),
		}
		activity.GpsBounds = []types.GpsPoint{
			{Lat: utils.Min(activity.Lats), Lon: utils.Min(activity.Lons)},
			{Lat: utils.Max(activity.Lats), Lon: utils.Max(activity.Lons)},
		}
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

package activityService

import (
	"MySportWeb/internal/pkg/metrics"
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
	"sort"
	"time"
)

// var decoderPool = sync.Pool{New: func() any { return decoder.New(nil) }}

// RestHRFunc returns the resting heart rate (bpm) of the user at a date, 0 when it's unknown
type RestHRFunc func(date time.Time) (uint16, error)

// InvalidFitError is a file that isn't a FIT activity the analysis can read: its message tells the user
// why. The other errors of the analysis are the server's (the file can't be opened, the database fails).
type InvalidFitError struct {
	err error
}

func (e *InvalidFitError) Error() string {
	return e.err.Error()
}

func (e *InvalidFitError) Unwrap() error {
	return e.err
}

// Analysis is what the analysis of a FIT file needs from the database, nil for the defaults: the analyses
// whose result doesn't depend on one leave it nil
type Analysis struct {
	// RestHR gives the resting heart rate of the training score and the zones, nil for DefaultRestHR
	RestHR RestHRFunc
	// Correction gives the altitude correction of the recording device, nil for none
	Correction CorrectionFunc
	// Weight gives the weight of the rider and the equipment, of the power estimated without power meter,
	// nil for DefaultRiderWeight and DefaultEquipmentWeight
	Weight WeightFunc
}

// AnalyzeTrimmed analyses a FIT file, ignoring what was recorded after trimEnd seconds since the start
// (0 for the whole recording), see TrimActivity.
// At most MaxConcurrentAnalyses files are analysed at a time, ErrBusy after waiting QueueTimeout.
func AnalyzeTrimmed(filePath string, user models.Users, equipment models.Equipments, trimEnd float64,
	opts Analysis) (models.Activity, error) {
	release, err := analysisSlots.acquire(QueueTimeout)
	if err != nil {
		metrics.Analyses.WithLabelValues("busy").Inc()
		return models.Activity{}, err
	}
	defer release()
	start := time.Now()
	result := "error"
	defer func() {
		// a panic (a file without session) counts as an error, then goes on to analyzeFile
		metrics.Analyses.WithLabelValues(result).Inc()
		metrics.AnalysisDuration.Observe(time.Since(start).Seconds())
	}()
	activity, err := analyzeTrimmed(filePath, user, equipment, trimEnd, opts)
	if err == nil {
		result = "ok"
	}
	return activity, err
}

// analyzeTrimmed is AnalyzeTrimmed, holding an analysis slot
func analyzeTrimmed(filePath string, user models.Users, equipment models.Equipments, trimEnd float64,
	opts Analysis) (models.Activity, error) {
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

	decoded := false
	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			return models.Activity{}, &InvalidFitError{err}
		}
		activity, err = decodeFit(fit, user, equipment, trimEnd, opts)
		if err != nil {
			return models.Activity{}, err
		}
		decoded = true
	}
	// an empty file, or one that doesn't start as a FIT file
	if !decoded {
		return models.Activity{}, &InvalidFitError{errors.New("not a FIT file")}
	}

	return activity, nil
}

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// func DecodeFit(fit *proto.FIT, user models.Users, equipment models.Equipments) (models.Activity, error) {
// 	return decodeFit(fit, user, equipment, 0, nil)
// }

func decodeFit(fit *proto.FIT, user models.Users, equipment models.Equipments, trimEnd float64,
	opts Analysis) (models.Activity, error) {
	restHR, correction := opts.RestHR, opts.Correction
	var activity models.Activity
	var err error
	fitActivity := filedef.NewActivity(fit.Messages...)
	if len(fitActivity.Sessions) == 0 {
		return models.Activity{}, &InvalidFitError{errors.New("no session in the FIT file")}
	}
	// the analysis starts at the first session
	sort.SliceStable(fitActivity.Sessions, func(i, j int) bool {
		return fitActivity.Sessions[i].StartTime.Before(fitActivity.Sessions[j].StartTime)
	})
	activity.RecordedDuration = recordedDuration(fitActivity)
	trimmed, err := trimFit(fitActivity, trimEnd)
	if err != nil {
		return models.Activity{}, err
	}
	if trimmed {
		activity.TrimEnd = trimEnd
	}
	activity.EngineVersion = vars.Engine_version
	activity.Device, activity.DeviceName = AnalyzeDevice(fitActivity)
	altitudeCorrection := models.AltitudeCorrectionNone
	if correction != nil && activity.DeviceName != "" {
		if altitudeCorrection, err = correction(activity.Device); err != nil {
			return models.Activity{}, err
		}
	}
	activity.User = user
	activity.Equipment = equipment
	activity.Lastimport = true
	if len(fitActivity.Sessions) == 1 {
		summarizeSession(&activity, fitActivity.Sessions[0])
	} else {
		legs := AnalyzeLegs(fitActivity.Sessions)
		if len(legs) == 0 {
			return models.Activity{}, &InvalidFitError{errors.New("no session other than transitions in the FIT file")}
		}
		activity.Legs = legs
		summarizeLegs(&activity, fitActivity.Sessions, legs)
		continuousDistances(fitActivity.Records, fitActivity.Sessions)
	}

	// the estimated power of the rides moves the rider and the equipment
	weight, err := totalWeight(opts.Weight, activity.Date, activity.Sport)
	if err != nil {
		return models.Activity{}, err
	}
	activity.TotalWeight = uint16(math.Round(weight))

	activity, err = AnalyzeRecords(fitActivity, activity, altitudeCorrection)
	if err != nil {
		return models.Activity{}, err
	}
	if len(activity.Legs) > 0 {
		estimateLegsPower(&activity)
	}
	// FIT times are in UTC: the time of the day is the user's one
	activity.Title = Title(activity.Sport, activity.Date, UserLocation(user.Timezone), activity.StartPosition)
	if len(fitActivity.Lengths) > 0 {
		activity.Lengths = AnalyzeLengths(fitActivity)
	}
	if activity.Sport == "cycling" {
		activity.Co2 = Commute(activity)
	}
	if err := setTrainingScore(&activity, fitActivity.Sessions, trimmed, restHR); err != nil {
		return models.Activity{}, err
	}
	if err := setHRZones(&activity, restHR); err != nil {
		return models.Activity{}, err
	}
	activity.BestEfforts = BestEfforts(fitActivity)

	return activity, nil
}

// summarizeSession sets the totals of a single-session activity
func summarizeSession(activity *models.Activity, session *mesgdef.Session) {
	activity.Sport = SportName(session.Sport)
	activity.Date = session.StartTime
	activity.Duration = session.TotalTimerTime / 1000
	if !math.IsNaN(session.TotalDistanceScaled()) {
		activity.Distance = session.TotalDistanceScaled()
	} else {
		activity.Distance = 0
	}
	if session.TotalCalories != math.MaxUint16 {
		activity.Calories = session.TotalCalories
	}
	if !math.IsNaN(session.EnhancedAvgSpeedScaled()) {
		activity.AvgSpeed = session.EnhancedAvgSpeedScaled()
	} else if !math.IsNaN(session.AvgSpeedScaled()) {
		activity.AvgSpeed = session.AvgSpeedScaled()
	} else {
		activity.AvgSpeed = 0
	}
	if !math.IsNaN(session.EnhancedMaxSpeedScaled()) {
		activity.MaxSpeed = session.EnhancedMaxSpeedScaled()
	} else if !math.IsNaN(session.MaxSpeedScaled()) {
		activity.MaxSpeed = session.MaxSpeedScaled()
	} else {
		activity.MaxSpeed = 0
	}
	if session.AvgCadence != math.MaxUint8 {
		activity.AvgRPM = session.AvgCadence
	}
	if session.AvgHeartRate != math.MaxUint8 {
		activity.AvgHR = session.AvgHeartRate
	}
	if session.AvgPower != math.MaxUint16 {
		activity.AvgPower = session.AvgPower
	}
	if session.AvgPower > 0 && session.AvgPower != math.MaxUint16 {
		activity.AvgPower = session.AvgPower
	}
	activity.TotalAscent = session.TotalAscent
	activity.TotalDescent = session.TotalDescent
}

// AnalyzeRecords computes the series of an activity from its records, its altitudes corrected by
// correction (models.AltitudeCorrectionNone...) when the ground elevation of its trace is known
func AnalyzeRecords(fitActivity *filedef.Activity, activity models.Activity, correction string) (models.Activity, error) {
	var startmeters, stopmeters, meters = 0.0, 0.0, 0.0
	var startaltitude, stopaltitude = 0.0, 0.0
	var avgSpeedT = 0.0
	var km = 0.0
	var tsstartkm time.Time
	var tsstartdist float64
	var counter = 0
	var recordsCount = len(fitActivity.Records)
	var altitudes, filtered []float64
	var startPosition, endPosition types.GpsPoint
	var running runningSeries

	distance := activity.Distance
	weight := activity.TotalWeight

	if recordsCount > 0 {
		tsstartkm = fitActivity.Records[0].Timestamp
		tsstartdist = fitActivity.Records[0].DistanceScaled()
	}
	if distance == 0 {
		distance = 1
	}
	// the altitude of every record, corrected, without gap: the profile, the elevation and the slope of the
	// estimated power come from them
	recorded, positions := recordAltitudes(fitActivity.Records)
	corrected, applied := correctAltitudes(recorded, positions, correction, Terrain)
	series := fillGaps(corrected)
	activity.ElevationCorrection = applied
	// the power is read or estimated per session: only the bike leg of a triathlon has some
	sessions := sessionCursor{sessions: fitActivity.Sessions}

	// the privacy zones are around the first and the last positions with a GPS fix: the first and the last
	// records often have none (no fix yet, or the device saving after the GPS stopped), and a zone around
	// an invalid position would hide nothing
	if first, last, found := gpsRange(fitActivity.Records); found {
		startPosition = recordPosition(fitActivity.Records[first])
		endPosition = recordPosition(fitActivity.Records[last])
	}
	for km < distance && counter < recordsCount-1 {
		stopkm := km + 1.0
		for km < stopkm && counter < recordsCount-1 {
			var deltaDistance, deltaAltitude float64
			stopmeters = meters + 10
			startmeters = fitActivity.Records[counter].DistanceScaled()
			startaltitude = series[counter]
			//record := fitActivity.Records[counter]
			var record *mesgdef.Record
			for meters < stopmeters && counter < recordsCount-1 {
				if counter < recordsCount-1 {
					record = fitActivity.Records[counter]
					// seconds since the start: sparse series (GPS, heart rate, cadence, power) skip the records
					// without a value, they keep their own time stamps to stay aligned with the others
					offset := record.Timestamp.Sub(fitActivity.Sessions[0].StartTime).Seconds()
					activity.TimeStamps = append(activity.TimeStamps, fmt.Sprintf("%f", offset))
					if record.HeartRate != math.MaxUint8 {
						activity.Hearts = append(activity.Hearts, uint16(record.HeartRate))
						activity.HeartTimeStamps = append(activity.HeartTimeStamps, offset)
					}
					if record.Temperature != math.MaxInt8 {
						activity.Temperatures = append(activity.Temperatures, int16(record.Temperature))
						activity.TemperatureTimeStamps = append(activity.TemperatureTimeStamps, offset)
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
					altitudes = append(altitudes, series[counter])
					if !math.IsNaN(record.EnhancedAltitudeScaled()) && !math.IsNaN(record.DistanceScaled()) {
						km = record.DistanceScaled() / 1000
						meters = record.DistanceScaled()
					}
					if record.PositionLat != math.MaxInt32 && record.PositionLong != math.MaxInt32 {
						activity.Lats = append(activity.Lats, utils.SemiCircleToDegres(record.PositionLat))
						activity.Lons = append(activity.Lons, utils.SemiCircleToDegres(record.PositionLong))
						currentPoint := types.GpsPoint{
							Lat: utils.SemiCircleToDegres(record.PositionLat),
							Lon: utils.SemiCircleToDegres(record.PositionLong),
						}
						activity.GpsPoints = append(activity.GpsPoints, currentPoint)
						activity.GpsTimeStamps = append(activity.GpsTimeStamps, offset)
					}
					// measured power, of any sport: running, rowing... Without power meter, only the cycling one is estimated
					if session := sessions.at(record.Timestamp); hasPowerMeter(session) {
						if record.Power != math.MaxUint16 {
							activity.Powers = append(activity.Powers, record.Power)
							activity.PowerTS = append(activity.PowerTS, fmt.Sprintf("%f", offset))
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
						activity.CadenceTimeStamps = append(activity.CadenceTimeStamps, offset)
					}
					running.add(record, offset)
				}
				counter++
			}
			stopaltitude = series[counter-1]
			if session := sessions.at(record.Timestamp); isCycling(session) && !hasPowerMeter(session) {
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
		// a triathlon has the power of its bike leg only, it goes on that leg
		if len(activity.Powers) > 0 && activity.Sport == "cycling" {
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
	activity.Metrics.Running = running.metrics()

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
	activity.PublicGpsPoints = PublicTrace(activity.GpsPoints, startPosition, endPosition, activity.User.SecurityDistance)
	activity.Altitudes = filtered
	if len(altitudes) > 2 {
		ComputeElevation(&activity, filtered)
	}
	if len(activity.GpsPoints) > 2 {
		activity.StartPosition = &startPosition
		activity.EndPosition = &endPosition
		activity.GpsCenter = &types.GpsPoint{
			Lat: utils.Avg(activity.Lats),
			Lon: utils.Avg(activity.Lons),
		}
		activity.GpsBounds = []types.GpsPoint{
			{Lat: utils.Min(activity.Lats), Lon: utils.Min(activity.Lons)},
			{Lat: utils.Max(activity.Lats), Lon: utils.Max(activity.Lons)},
		}
		activity.SimplifiedGpsPoints = SimplifyTrace(activity.GpsPoints)
	}
	return activity, nil
}

// PublicTrace returns the points of a trace the others may see: the ones out of the privacy zones, circles
// of securityDistance meters around the start and the end (where the user lives or works). The radius is
// doubled for a loop, whose start and end are close. start and end are the first and the last positions
// with a GPS fix.
func PublicTrace(points []types.GpsPoint, start, end types.GpsPoint, securityDistance uint16) types.GpsPointArray {
	var public types.GpsPointArray
	if len(points) == 0 {
		return public
	}
	radius := float64(securityDistance) / 1000 // Haversine is in km
	if utils.Haversine(start, end) < radius {
		radius *= 2
	}
	for _, point := range points {
		if utils.Haversine(start, point) > radius && utils.Haversine(end, point) > radius {
			public = append(public, point)
		}
	}
	return public
}

// ValidPosition tells whether a position is on Earth: the positions read from records without GPS fix
// (stored by the engines before 5) are around (180, 180)
func ValidPosition(p *types.GpsPoint) bool {
	return p != nil && math.Abs(p.Lat) <= 90 && math.Abs(p.Lon) < 180
}

// hasPosition tells whether a record has a GPS fix
func hasPosition(record *mesgdef.Record) bool {
	return record.PositionLat != math.MaxInt32 && record.PositionLong != math.MaxInt32
}

func recordPosition(record *mesgdef.Record) types.GpsPoint {
	return types.GpsPoint{
		Lat: utils.SemiCircleToDegres(record.PositionLat),
		Lon: utils.SemiCircleToDegres(record.PositionLong),
	}
}

// gpsRange returns the indexes of the first and the last records with a GPS fix, false when none has one
func gpsRange(records []*mesgdef.Record) (first, last int, found bool) {
	first = -1
	for i, record := range records {
		if hasPosition(record) {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	return first, last, first >= 0
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

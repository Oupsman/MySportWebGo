package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"math"
	"slices"

	"github.com/muktihari/fit/profile/mesgdef"
)

// Ground gives the elevation of the ground at a position, NaN when it is unknown (see elevation.Source)
type Ground interface {
	At(lat, lon float64) float64
}

// Terrain gives the ground elevations of the altitude corrections; nil leaves the altitudes as recorded
var Terrain Ground

// CorrectionFunc returns the altitude correction (models.AltitudeCorrectionNone...) of the device that
// recorded an activity
type CorrectionFunc func(device types.Device) (string, error)

// DeviceCorrection returns the altitude correction of the user's devices
func DeviceCorrection(db *models.DB, userID uint) CorrectionFunc {
	return func(device types.Device) (string, error) {
		return db.DeviceAltitudeCorrection(userID, device)
	}
}

// minGroundCoverage is the share of the GPS fixes whose ground elevation must be known to correct the
// altitudes: below, the tiles are missing for most of the trace, the recorded altitudes are kept
const minGroundCoverage = 0.5

// recordAltitude is the altitude of a record, in meters: the enhanced one, else the other, else NaN
func recordAltitude(record *mesgdef.Record) float64 {
	if altitude := record.EnhancedAltitudeScaled(); !math.IsNaN(altitude) {
		return altitude
	}
	return record.AltitudeScaled()
}

// recordAltitudes returns the altitude and the position of every record: NaN without them
func recordAltitudes(records []*mesgdef.Record) ([]float64, []types.GpsPoint) {
	altitudes := make([]float64, len(records))
	positions := make([]types.GpsPoint, len(records))
	for i, record := range records {
		altitudes[i] = recordAltitude(record)
		if hasPosition(record) {
			positions[i] = recordPosition(record)
		} else {
			positions[i] = types.GpsPoint{Lat: math.NaN(), Lon: math.NaN()}
		}
	}
	return altitudes, positions
}

// correctAltitudes applies an altitude correction to the altitudes recorded at positions (NaN without),
// with the ground elevations of ground. It returns the altitudes, NaN where they are unknown, and the
// correction applied: none without ground, when the ground of too few positions is known, or for an
// indoor activity.
//   - replace: the ground elevation under each position
//   - calibrate: the recorded altitudes shifted by the median difference with the ground: the profile of
//     the barometer, at the level of the ground. Without any recorded altitude, the ground ones replace them.
func correctAltitudes(altitudes []float64, positions []types.GpsPoint, correction string, ground Ground) ([]float64, string) {
	if ground == nil || (correction != models.AltitudeCorrectionReplace && correction != models.AltitudeCorrectionCalibrate) {
		return altitudes, models.AltitudeCorrectionNone
	}
	elevations := make([]float64, len(positions))
	fixes, known := 0, 0
	for i, p := range positions {
		elevations[i] = math.NaN()
		if math.IsNaN(p.Lat) {
			continue
		}
		fixes++
		if elevations[i] = ground.At(p.Lat, p.Lon); !math.IsNaN(elevations[i]) {
			known++
		}
	}
	if known < 2 || float64(known) < minGroundCoverage*float64(fixes) {
		return altitudes, models.AltitudeCorrectionNone
	}

	if correction == models.AltitudeCorrectionCalibrate {
		var differences []float64
		recorded := false
		for i, altitude := range altitudes {
			if math.IsNaN(altitude) {
				continue
			}
			recorded = true
			if !math.IsNaN(elevations[i]) {
				differences = append(differences, elevations[i]-altitude)
			}
		}
		if recorded {
			if len(differences) == 0 {
				return altitudes, models.AltitudeCorrectionNone
			}
			// the median: the bridges and the tunnels, where the ground isn't where the device is, don't move it
			offset := median(differences)
			calibrated := make([]float64, len(altitudes))
			for i, altitude := range altitudes {
				calibrated[i] = altitude + offset
			}
			return calibrated, models.AltitudeCorrectionCalibrate
		}
	}
	return elevations, models.AltitudeCorrectionReplace
}

// fillGaps replaces the NaN of values by the linear interpolation of the values around them, the first and
// the last known values before and after them; 0 when no value is known
func fillGaps(values []float64) []float64 {
	filled := make([]float64, len(values))
	previous := -1 // index of the last known value
	for i, value := range values {
		if math.IsNaN(value) {
			continue
		}
		filled[i] = value
		switch {
		case previous < 0:
			for j := 0; j < i; j++ {
				filled[j] = value
			}
		case previous < i-1:
			for j := previous + 1; j < i; j++ {
				ratio := float64(j-previous) / float64(i-previous)
				filled[j] = values[previous] + (value-values[previous])*ratio
			}
		}
		previous = i
	}
	if previous >= 0 {
		for j := previous + 1; j < len(values); j++ {
			filled[j] = values[previous]
		}
	}
	return filled
}

func median(values []float64) float64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2
	}
	return sorted[middle]
}

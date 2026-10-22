package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/utils"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

// multisportNames name a multi-session activity after the sports of its legs, in order.
// The other mixes are "multisport".
var multisportNames = map[string]string{
	"swimming,cycling,running": "triathlon",
	"running,cycling,running":  "duathlon",
	"swimming,running":         "aquathlon",
}

// valid returns the scaled value of a FIT field, 0 when the field is missing
func valid(value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	return value
}

func validUint8(value uint8) uint8 {
	if value == basetype.Uint8Invalid {
		return 0
	}
	return value
}

func validUint16(value uint16) uint16 {
	if value == basetype.Uint16Invalid {
		return 0
	}
	return value
}

func sessionAvgSpeed(session *mesgdef.Session) float64 {
	if speed := session.EnhancedAvgSpeedScaled(); !math.IsNaN(speed) {
		return speed
	}
	return valid(session.AvgSpeedScaled())
}

func sessionMaxSpeed(session *mesgdef.Session) float64 {
	if speed := session.EnhancedMaxSpeedScaled(); !math.IsNaN(speed) {
		return speed
	}
	return valid(session.MaxSpeedScaled())
}

// AnalyzeLegs returns the legs of the sessions, in time order. The transitions are folded into the leg
// after them; trailing ones only count in the duration of the activity.
func AnalyzeLegs(sessions []*mesgdef.Session) []types.Leg {
	legs := []types.Leg{}
	start := sessions[0].StartTime
	transition := 0.0
	for _, session := range sessions {
		duration := valid(session.TotalTimerTimeScaled())
		if session.Sport == typedef.SportTransition {
			transition += duration
			continue
		}
		legs = append(legs, types.Leg{
			Sport:          session.Sport.String(),
			StartOffset:    session.StartTime.Sub(start).Seconds(),
			Duration:       duration,
			Distance:       valid(session.TotalDistanceScaled()),
			AvgSpeed:       sessionAvgSpeed(session),
			MaxSpeed:       sessionMaxSpeed(session),
			AvgHR:          validUint8(session.AvgHeartRate),
			MaxHR:          validUint8(session.MaxHeartRate),
			AvgCadence:     validUint8(session.AvgCadence),
			AvgPower:       validUint16(session.AvgPower),
			Calories:       validUint16(session.TotalCalories),
			Ascent:         validUint16(session.TotalAscent),
			Descent:        validUint16(session.TotalDescent),
			TransitionTime: transition,
		})
		transition = 0
	}
	return legs
}

// MultisportName returns the sport of an activity made of legs: the sport of the legs when they all
// have the same, else the name of the mix (triathlon...)
func MultisportName(legs []types.Leg) string {
	sports := make([]string, len(legs))
	same := true
	for i, leg := range legs {
		sports[i] = leg.Sport
		same = same && leg.Sport == legs[0].Sport
	}
	if same {
		return legs[0].Sport
	}
	if name, ok := multisportNames[strings.Join(sports, ",")]; ok {
		return name
	}
	return "multisport"
}

// summarizeLegs sets the totals of a multi-session activity. The duration, distance, calories and
// climb include the transitions: they are part of the race. The averages of cadence and power only make
// sense within one sport, they stay on the legs otherwise.
func summarizeLegs(activity *models.Activity, sessions []*mesgdef.Session, legs []types.Leg) {
	activity.Sport = MultisportName(legs)
	activity.Date = sessions[0].StartTime
	duration := 0.0
	for _, session := range sessions {
		duration += valid(session.TotalTimerTimeScaled())
		activity.Distance += valid(session.TotalDistanceScaled())
		activity.Calories += validUint16(session.TotalCalories)
		activity.TotalAscent += validUint16(session.TotalAscent)
		activity.TotalDescent += validUint16(session.TotalDescent)
	}
	activity.Duration = uint32(duration)
	if duration > 0 {
		activity.AvgSpeed = activity.Distance / duration
	}

	oneSport := activity.Sport == legs[0].Sport
	var hrTime, hrSum, cadenceTime, cadenceSum, powerTime, powerSum float64
	for _, leg := range legs {
		activity.MaxSpeed = math.Max(activity.MaxSpeed, leg.MaxSpeed)
		if leg.AvgHR > 0 {
			hrTime += leg.Duration
			hrSum += float64(leg.AvgHR) * leg.Duration
		}
		if oneSport && leg.AvgCadence > 0 {
			cadenceTime += leg.Duration
			cadenceSum += float64(leg.AvgCadence) * leg.Duration
		}
		if oneSport && leg.AvgPower > 0 {
			powerTime += leg.Duration
			powerSum += float64(leg.AvgPower) * leg.Duration
		}
	}
	if hrTime > 0 {
		activity.AvgHR = uint8(math.Round(hrSum / hrTime))
	}
	if cadenceTime > 0 {
		activity.AvgRPM = uint8(math.Round(cadenceSum / cadenceTime))
	}
	if powerTime > 0 {
		activity.AvgPower = uint16(math.Round(powerSum / powerTime))
	}
}

// sessionCursor finds the session of records read in time order, the sessions are in time order
type sessionCursor struct {
	sessions []*mesgdef.Session
	index    int
}

// at returns the last session started at t: the records of a transition belong to it
func (c *sessionCursor) at(t time.Time) *mesgdef.Session {
	for c.index+1 < len(c.sessions) && !t.Before(c.sessions[c.index+1].StartTime) {
		c.index++
	}
	return c.sessions[c.index]
}

func isCycling(session *mesgdef.Session) bool {
	return session.Sport == typedef.SportCycling
}

func hasPowerMeter(session *mesgdef.Session) bool {
	return session.AvgPower != basetype.Uint16Invalid
}

// estimateLegsPower sets the power of the bike legs recorded without power meter, from the power
// estimated on their records by AnalyzeRecords
func estimateLegsPower(activity *models.Activity) {
	for i := range activity.Legs {
		leg := &activity.Legs[i]
		if leg.Sport != "cycling" || leg.AvgPower > 0 {
			continue
		}
		end := math.Inf(1)
		if i+1 < len(activity.Legs) {
			end = activity.Legs[i+1].StartOffset
		}
		var powers []uint16
		for j, ts := range activity.PowerTS {
			offset, err := strconv.ParseFloat(ts, 64)
			if err == nil && j < len(activity.Powers) && offset >= leg.StartOffset && offset < end {
				powers = append(powers, activity.Powers[j])
			}
		}
		if len(powers) > 0 {
			// normalized like the power of a single ride
			leg.AvgPower = uint16(utils.NormalizedAvgPositive(powers))
			leg.PowerEstimated = true
		}
	}
}

// continuousDistances makes the distances of the records grow over the whole file: devices restart them
// at 0 on each session, the analysis needs one growing distance to cut the activity in kilometers.
// The sessions are in time order.
func continuousDistances(records []*mesgdef.Record, sessions []*mesgdef.Session) {
	var offset, previous uint32
	cursor := sessionCursor{sessions: sessions}
	session := sessions[0]
	for _, record := range records {
		if record.Distance == basetype.Uint32Invalid {
			continue
		}
		// the first distance of a session, below the last one of the previous session: a restart
		if next := cursor.at(record.Timestamp); next != session {
			session = next
			if record.Distance < previous {
				offset += previous
			}
		}
		previous = record.Distance
		record.Distance += offset
	}
}

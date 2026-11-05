package activityService

import (
	"MySportWeb/internal/pkg/models"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// TrimMinDuration is the shortest activity (in seconds) trimming can leave
const TrimMinDuration = 60

// ErrTrimTooShort is returned when trimming would leave less than TrimMinDuration or no record
var ErrTrimTooShort = errors.New("the trimmed activity would be too short")

// ErrNoFitFile is returned when an activity can't be trimmed because its FIT file isn't known
var ErrNoFitFile = errors.New("the FIT file of this activity is missing")

// recordedDuration returns the seconds between the start of the first session and the last record
func recordedDuration(fit *filedef.Activity) float64 {
	start := fit.Sessions[0].StartTime
	last := start
	for _, record := range fit.Records {
		if record.Timestamp.After(last) {
			last = record.Timestamp
		}
	}
	for _, session := range fit.Sessions {
		if elapsed := valid(session.TotalElapsedTimeScaled()); elapsed > 0 {
			if end := session.StartTime.Add(seconds(elapsed)); end.After(last) {
				last = end
			}
		}
	}
	return last.Sub(start).Seconds()
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

// trimFit drops what the FIT activity recorded after trimEnd seconds since the start of its first session,
// and computes again the totals of the sessions it cuts. The sessions are in time order.
// It returns false when trimEnd is at or after the end of the recording: there is nothing to trim.
func trimFit(fit *filedef.Activity, trimEnd float64) (bool, error) {
	if trimEnd <= 0 || trimEnd >= recordedDuration(fit) {
		return false, nil
	}
	if trimEnd < TrimMinDuration {
		return false, ErrTrimTooShort
	}
	start := fit.Sessions[0].StartTime
	end := start.Add(seconds(trimEnd))

	records := fit.Records[:0]
	for _, record := range fit.Records {
		if !record.Timestamp.After(end) {
			records = append(records, record)
		}
	}
	if len(records) < 2 {
		return false, ErrTrimTooShort
	}
	fit.Records = records

	lengths := fit.Lengths[:0]
	for _, length := range fit.Lengths {
		if !length.Timestamp.After(end) {
			lengths = append(lengths, length)
		}
	}
	fit.Lengths = lengths

	laps := fit.Laps[:0]
	for _, lap := range fit.Laps {
		if lap.StartTime.Before(end) {
			laps = append(laps, lap)
		}
	}
	fit.Laps = laps

	sessions := fit.Sessions[:0]
	for _, session := range fit.Sessions {
		if session.StartTime.Before(end) {
			sessions = append(sessions, session)
		}
	}
	fit.Sessions = sessions

	for i, session := range fit.Sessions {
		next := end
		if i+1 < len(fit.Sessions) {
			next = fit.Sessions[i+1].StartTime
		}
		elapsed := valid(session.TotalElapsedTimeScaled())
		if elapsed > 0 && !session.StartTime.Add(seconds(elapsed)).After(end) {
			continue
		}
		var sessionRecords []*mesgdef.Record
		for _, record := range fit.Records {
			// the last session keeps the record at end
			if !record.Timestamp.Before(session.StartTime) && (record.Timestamp.Before(next) || next.Equal(end)) {
				sessionRecords = append(sessionRecords, record)
			}
		}
		trimSession(session, sessionRecords, timerTime(fit.Events, session.StartTime, end), end)
	}
	return true, nil
}

// timerTime returns the time the timer ran between from and to, from the timer events: the pauses
// don't count. Without timer event, the whole time counts.
func timerTime(events []*mesgdef.Event, from, to time.Time) time.Duration {
	var timers []*mesgdef.Event
	for _, event := range events {
		if event.Event == typedef.EventTimer {
			timers = append(timers, event)
		}
	}
	if len(timers) == 0 {
		return to.Sub(from)
	}
	sort.SliceStable(timers, func(i, j int) bool { return timers[i].Timestamp.Before(timers[j].Timestamp) })
	clamp := func(t time.Time) time.Time {
		if t.Before(from) {
			return from
		}
		if t.After(to) {
			return to
		}
		return t
	}
	var total time.Duration
	// a file starting with a stop event was running from the start
	running := timers[0].EventType != typedef.EventTypeStart
	since := from
	for _, event := range timers {
		t := clamp(event.Timestamp)
		switch event.EventType {
		case typedef.EventTypeStart:
			if !running {
				running = true
				since = t
			}
		case typedef.EventTypeStop, typedef.EventTypeStopAll, typedef.EventTypeStopDisable, typedef.EventTypeStopDisableAll:
			if running {
				total += t.Sub(since)
				running = false
			}
		}
	}
	if running {
		total += to.Sub(since)
	}
	return total
}

// trimSession sets the totals of a session ending at end from its records, which are in time order.
// The calories are cut in proportion of the timer time: the records don't tell them.
func trimSession(session *mesgdef.Session, records []*mesgdef.Record, timer time.Duration, end time.Time) {
	oldTimer := valid(session.TotalTimerTimeScaled())
	if oldTimer == 0 {
		oldTimer = valid(session.TotalElapsedTimeScaled())
	}
	timerSeconds := timer.Seconds()
	session.Timestamp = end
	session.TotalElapsedTime = uint32(end.Sub(session.StartTime).Milliseconds())
	session.TotalTimerTime = uint32(timer.Milliseconds())

	if session.TotalCalories != basetype.Uint16Invalid && oldTimer > 0 {
		session.TotalCalories = uint16(math.Round(float64(session.TotalCalories) * math.Min(timerSeconds/oldTimer, 1)))
	}

	var firstDistance, lastDistance uint32 = basetype.Uint32Invalid, basetype.Uint32Invalid
	var maxSpeed float64
	var hrSum, hrCount, cadenceSum, cadenceCount, powerSum, powerCount int
	var maxHR uint8
	var maxPower uint16
	var ascent, descent float64
	reference := math.NaN()
	for _, record := range records {
		if record.Distance != basetype.Uint32Invalid {
			if firstDistance == basetype.Uint32Invalid {
				firstDistance = record.Distance
			}
			lastDistance = record.Distance
		}
		speed := record.EnhancedSpeedScaled()
		if math.IsNaN(speed) {
			speed = record.SpeedScaled()
		}
		if !math.IsNaN(speed) && speed > maxSpeed {
			maxSpeed = speed
		}
		if record.HeartRate != basetype.Uint8Invalid {
			hrSum += int(record.HeartRate)
			hrCount++
			maxHR = max(maxHR, record.HeartRate)
		}
		if record.Cadence != basetype.Uint8Invalid && record.Cadence > 0 {
			cadenceSum += int(record.Cadence)
			cadenceCount++
		}
		if record.Power != basetype.Uint16Invalid {
			powerSum += int(record.Power)
			powerCount++
			maxPower = max(maxPower, record.Power)
		}
		// the climb, ignoring the variations below 3 m: the noise of the altimeter
		altitude := record.EnhancedAltitudeScaled()
		if math.IsNaN(altitude) {
			altitude = record.AltitudeScaled()
		}
		if math.IsNaN(altitude) {
			continue
		}
		if math.IsNaN(reference) {
			reference = altitude
		} else if altitude-reference >= 3 {
			ascent += altitude - reference
			reference = altitude
		} else if reference-altitude >= 3 {
			descent += reference - altitude
			reference = altitude
		}
	}

	if lastDistance != basetype.Uint32Invalid {
		session.TotalDistance = lastDistance - firstDistance
		distance := float64(session.TotalDistance) / 100
		avgSpeed := 0.0
		if timerSeconds > 0 {
			avgSpeed = distance / timerSeconds
		}
		session.EnhancedAvgSpeed = uint32(math.Round(avgSpeed * 1000))
		session.AvgSpeed = speedUint16(avgSpeed)
		session.EnhancedMaxSpeed = uint32(math.Round(maxSpeed * 1000))
		session.MaxSpeed = speedUint16(maxSpeed)
	}
	if hrCount > 0 {
		session.AvgHeartRate = uint8(math.Round(float64(hrSum) / float64(hrCount)))
		session.MaxHeartRate = maxHR
	}
	if cadenceCount > 0 {
		session.AvgCadence = uint8(math.Round(float64(cadenceSum) / float64(cadenceCount)))
	}
	// only with a power meter: without it, the power is estimated by the analysis
	if session.AvgPower != basetype.Uint16Invalid && powerCount > 0 {
		session.AvgPower = uint16(math.Round(float64(powerSum) / float64(powerCount)))
		session.MaxPower = maxPower
	}
	if session.TotalAscent != basetype.Uint16Invalid && !math.IsNaN(reference) {
		session.TotalAscent = uint16(math.Round(ascent))
	}
	if session.TotalDescent != basetype.Uint16Invalid && !math.IsNaN(reference) {
		session.TotalDescent = uint16(math.Round(descent))
	}
}

// speedUint16 returns a speed in the scale of the 16 bits speed fields, invalid when it doesn't fit
func speedUint16(speed float64) uint16 {
	value := math.Round(speed * 1000)
	if value >= float64(basetype.Uint16Invalid) {
		return basetype.Uint16Invalid
	}
	return uint16(value)
}

// TrimActivity analyses again the FIT file of the activity, ignoring what was recorded after trimEnd seconds
// since its start (the device was not stopped at the end), 0 to keep the whole recording. The FIT file is
// left as it is. What the user chose (title, equipment, visibility...) is kept; the route is matched again
// since the trace changed, and the thumbnail drawn again.
func TrimActivity(db *models.DB, activity *models.Activity, trimEnd float64, logger zerolog.Logger) (*models.Activity, error) {
	if activity.FilePath == "" {
		return nil, ErrNoFitFile
	}
	analysed, err := analyzeFile(activity.FilePath, activity.User, trimEnd)
	if err != nil {
		return nil, err
	}

	trimmed := analysed
	trimmed.ID = activity.ID
	trimmed.CreatedAt = activity.CreatedAt
	trimmed.DeletedAt = activity.DeletedAt
	trimmed.User = activity.User
	trimmed.UserID = activity.UserID
	trimmed.Title = activity.Title
	trimmed.Filename = activity.Filename
	trimmed.FilePath = activity.FilePath
	trimmed.IsCommute = activity.IsCommute
	trimmed.Timezone = activity.Timezone
	trimmed.Masked = activity.Masked
	trimmed.EquipmentID = activity.EquipmentID
	trimmed.Serialnumber = activity.Serialnumber
	trimmed.Lastimport = activity.Lastimport
	trimmed.Visibility = activity.Visibility
	trimmed.CanComments = activity.CanComments
	trimmed.Medias = activity.Medias
	trimmed.TotalWeight = activity.TotalWeight
	trimmed.UserAge = activity.UserAge
	trimmed.Thumbnail = activity.Thumbnail
	trimmed.RouteID = nil

	err = db.Transaction(func(tx *gorm.DB) error {
		txDB := &models.DB{DB: *tx}
		if err := txDB.LockUserRoutes(trimmed.UserID); err != nil {
			return err
		}
		if activity.RouteID != nil {
			if err := txDB.LeaveRoute(*activity.RouteID); err != nil {
				return err
			}
		}
		if err := assignRoute(txDB, &trimmed); err != nil {
			return err
		}
		// a ride, or a route known as a commute, saves the CO2 of its new distance
		trimmed.Co2 = 0
		if trimmed.Sport == models.CommuteSport || trimmed.IsCommute {
			trimmed.Co2 = Commute(trimmed)
		}
		// the association, set by assignRoute for a commute, would override EquipmentID when saving
		trimmed.Equipment = models.Equipments{}
		return txDB.UpdateActivity(&trimmed)
	})
	if err != nil {
		return nil, err
	}
	// the activity is saved: a missing thumbnail only shows the old trace in the lists
	if err := GenerateThumbnail(trimmed); err != nil {
		logger.Warn().Err(err).Str("activity", trimmed.ID.String()).Msg("thumbnail not drawn again after trimming")
	}
	return &trimmed, nil
}

package models

import (
	"MySportWeb/internal/pkg/types"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Segment is a stretch of a ride a user selected: the passes of their activities on it are compared
type Segment struct {
	ID               uuid.UUID           `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt        time.Time           `json:"created_at"`
	UserID           uint                `json:"-" gorm:"not null"`
	Name             string              `json:"name" gorm:"not null"`
	Sport            string              `json:"sport" gorm:"not null"`
	Points           types.GpsPointArray `json:"points" gorm:"not null"`
	Distance         float64             `json:"distance" gorm:"not null"`       // meters
	ElevationGain    float64             `json:"elevation_gain" gorm:"not null"` // meters climbed
	MinLat           float64             `json:"-" gorm:"not null"`
	MinLon           float64             `json:"-" gorm:"not null"`
	MaxLat           float64             `json:"-" gorm:"not null"`
	MaxLon           float64             `json:"-" gorm:"not null"`
	SourceActivityID *uuid.UUID          `json:"source_activity_id" gorm:"type:uuid"`
	// the version of the matching rules its passes were found with, 0 until they are
	MatchVersion int `json:"-" gorm:"not null;default:0"`
}

// SegmentEffort is a pass of an activity on a segment
type SegmentEffort struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	SegmentID    uuid.UUID `gorm:"type:uuid;not null" json:"segment_id"`
	ActivityID   uuid.UUID `gorm:"type:uuid;not null" json:"activity_id"`
	StartOffset  float64   `json:"start_offset" gorm:"not null"` // seconds since the start of the activity
	Elapsed      float64   `json:"elapsed" gorm:"not null"`      // seconds
	AvgHeartRate *float64  `json:"avg_heart_rate"`
	AvgPower     *float64  `json:"avg_power"`
}

// SegmentSummary is a segment of the list: its passes, the best one, the latest one
type SegmentSummary struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Sport         string     `json:"sport"`
	Distance      float64    `json:"distance"`
	ElevationGain float64    `json:"elevation_gain"`
	Efforts       int64      `json:"efforts"`
	Best          *float64   `json:"best"`      // seconds, nil without pass
	BestDate      *time.Time `json:"best_date"` // the date of its activity
	LastDate      *time.Time `json:"last_date"`
}

// RankedEffort is a pass on a segment with its activity, ranked among the passes on the segment
type RankedEffort struct {
	SegmentEffort
	Rank          int       `json:"rank"`
	Efforts       int       `json:"efforts"` // the passes on the segment: rank out of them
	ActivityTitle string    `json:"activity_title"`
	Date          time.Time `json:"date"` // of the activity
	SegmentName   string    `json:"segment_name"`
	Distance      float64   `json:"distance"` // of the segment
}

func (db *DB) CreateSegment(segment *Segment) error {
	return db.Create(segment).Error
}

// GetSegment returns a segment of a user, gorm.ErrRecordNotFound for another user's
func (db *DB) GetSegment(userID uint, id uuid.UUID) (Segment, error) {
	var segment Segment
	err := db.Where("id = ? AND user_id = ?", id, userID).Take(&segment).Error
	return segment, err
}

// GetSegmentsList returns the segments of a user with their passes, the most recently ridden first
func (db *DB) GetSegmentsList(userID uint) ([]SegmentSummary, error) {
	segments := []SegmentSummary{}
	err := db.Raw(`SELECT s.id, s.name, s.sport, s.distance, s.elevation_gain, COUNT(e.id) AS efforts,
			MIN(e.elapsed) AS best,
			(SELECT a.date FROM segment_efforts b JOIN activities a ON a.id = b.activity_id
			 WHERE b.segment_id = s.id ORDER BY b.elapsed, a.date LIMIT 1) AS best_date,
			MAX(a.date) AS last_date
		FROM segments s
		LEFT JOIN segment_efforts e ON e.segment_id = s.id
		LEFT JOIN activities a ON a.id = e.activity_id
		WHERE s.user_id = ?
		GROUP BY s.id
		ORDER BY MAX(a.date) DESC NULLS LAST, s.created_at DESC`, userID).Scan(&segments).Error
	return segments, err
}

// rankedEfforts selects the passes with their rank on their segment, of the segments of a user
const rankedEfforts = `SELECT * FROM (SELECT e.*, a.title AS activity_title, a.date, s.name AS segment_name,
		s.distance, RANK() OVER (PARTITION BY e.segment_id ORDER BY e.elapsed) AS rank,
		COUNT(*) OVER (PARTITION BY e.segment_id) AS efforts
	FROM segment_efforts e JOIN segments s ON s.id = e.segment_id JOIN activities a ON a.id = e.activity_id
	WHERE s.user_id = ?) ranked `

// GetSegmentEfforts returns the passes on a segment of a user, the fastest first
func (db *DB) GetSegmentEfforts(userID uint, segmentID uuid.UUID) ([]RankedEffort, error) {
	efforts := []RankedEffort{}
	err := db.Raw(rankedEfforts+"WHERE segment_id = ? ORDER BY elapsed, date", userID, segmentID).Scan(&efforts).Error
	return efforts, err
}

// GetActivitySegmentEfforts returns the passes of an activity of a user on their segments, in their order
func (db *DB) GetActivitySegmentEfforts(userID uint, activityID uuid.UUID) ([]RankedEffort, error) {
	efforts := []RankedEffort{}
	err := db.Raw(rankedEfforts+"WHERE activity_id = ? ORDER BY start_offset", userID, activityID).Scan(&efforts).Error
	return efforts, err
}

// RenameSegment renames a segment of a user; false when the user has no such segment
func (db *DB) RenameSegment(userID uint, id uuid.UUID, name string) (bool, error) {
	result := db.Model(&Segment{}).Where("id = ? AND user_id = ?", id, userID).Update("name", name)
	return result.RowsAffected > 0, result.Error
}

// DeleteSegment deletes a segment of a user and its passes; false when the user has no such segment
func (db *DB) DeleteSegment(userID uint, id uuid.UUID) (bool, error) {
	result := db.Where("id = ? AND user_id = ?", id, userID).Delete(&Segment{})
	return result.RowsAffected > 0, result.Error
}

// ReplaceSegmentEfforts replaces the passes of an activity on a segment
func (db *DB) ReplaceSegmentEfforts(segmentID, activityID uuid.UUID, efforts []SegmentEffort) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("segment_id = ? AND activity_id = ?", segmentID, activityID).Delete(&SegmentEffort{}).Error; err != nil {
			return err
		}
		if len(efforts) == 0 {
			return nil
		}
		return tx.Create(&efforts).Error
	})
}

// ReplaceActivityEfforts replaces the passes of an activity on all the segments
func (db *DB) ReplaceActivityEfforts(activityID uuid.UUID, efforts []SegmentEffort) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("activity_id = ?", activityID).Delete(&SegmentEffort{}).Error; err != nil {
			return err
		}
		if len(efforts) == 0 {
			return nil
		}
		return tx.Create(&efforts).Error
	})
}

// GetSegmentByID returns a segment, whoever its user: for the background jobs
func (db *DB) GetSegmentByID(id uuid.UUID) (Segment, error) {
	var segment Segment
	err := db.Where("id = ?", id).Take(&segment).Error
	return segment, err
}

// GetSegmentsBelowMatchVersion returns the ids of the segments whose passes were found with matching rules
// older than version, the most recently ridden first
func (db *DB) GetSegmentsBelowMatchVersion(version int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Raw(`SELECT s.id FROM segments s
		LEFT JOIN segment_efforts e ON e.segment_id = s.id
		LEFT JOIN activities a ON a.id = e.activity_id
		WHERE s.match_version < ?
		GROUP BY s.id
		ORDER BY MAX(a.date) DESC NULLS LAST, s.created_at DESC, s.id`, version).Scan(&ids).Error
	return ids, err
}

// SetSegmentMatchVersion records the version of the matching rules the passes of a segment were found with
func (db *DB) SetSegmentMatchVersion(id uuid.UUID, version int) error {
	return db.Model(&Segment{}).Where("id = ?", id).Update("match_version", version).Error
}

// DeleteSegmentEffortsExcept deletes the passes on a segment of the activities not in activityIDs: the ones
// that can't pass on it any more
func (db *DB) DeleteSegmentEffortsExcept(segmentID uuid.UUID, activityIDs []uuid.UUID) error {
	query := db.Where("segment_id = ?", segmentID)
	if len(activityIDs) > 0 {
		// NOT IN of an empty list would keep them all
		query = query.Where("activity_id NOT IN ?", activityIDs)
	}
	return query.Delete(&SegmentEffort{}).Error
}

// GetSegmentsAround returns the segments of a user of a sport whose box meets a box: the ones an activity
// in that box may pass on
func (db *DB) GetSegmentsAround(userID uint, sport string, min, max types.GpsPoint) ([]Segment, error) {
	var segments []Segment
	err := db.Where("user_id = ? AND sport = ? AND min_lat <= ? AND max_lat >= ? AND min_lon <= ? AND max_lon >= ?",
		userID, sport, max.Lat, min.Lat, max.Lon, min.Lon).Find(&segments).Error
	return segments, err
}

// GetActivitiesAround returns the ids of the activities of a user of a sport whose box (gps_bounds) meets a
// box: the ones that may pass on a segment in it
func (db *DB) GetActivitiesAround(userID uint, sport string, min, max types.GpsPoint) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Raw(`SELECT id FROM (SELECT id, convert_from(gps_bounds, 'UTF8')::jsonb AS b FROM activities
			WHERE user_id = ? AND sport = ? AND gps_bounds IS NOT NULL) a
		WHERE jsonb_typeof(b) = 'array' AND jsonb_array_length(b) = 2
			AND (b->0->>'lat')::float8 <= ? AND (b->1->>'lat')::float8 >= ?
			AND (b->0->>'lon')::float8 <= ? AND (b->1->>'lon')::float8 >= ?
		ORDER BY id`, userID, sport, max.Lat, min.Lat, max.Lon, min.Lon).Scan(&ids).Error
	return ids, err
}

// SegmentTrack is what matching an activity with the segments needs: its trace with its times, and the
// series averaged on a pass
type SegmentTrack struct {
	ID              uuid.UUID
	UserID          uint
	Sport           string
	GpsPoints       types.GpsPointArray
	GpsTimeStamps   types.FloatArray
	GpsBounds       types.GpsPointArray
	Hearts          types.Uint16Array
	HeartTimeStamps types.FloatArray
	Powers          types.Uint16Array
	PowerTS         types.MultiString
	TimeStamps      types.MultiString
	Altitudes       types.FloatArray
}

// GetSegmentTrack returns what matching an activity with the segments needs
func (db *DB) GetSegmentTrack(id uuid.UUID) (SegmentTrack, error) {
	var track SegmentTrack
	err := db.Table("activities").Select("id, user_id, sport, gps_points, gps_time_stamps, gps_bounds, hearts, "+
		"heart_time_stamps, powers, power_ts, time_stamps, altitudes").Where("id = ?", id).Take(&track).Error
	return track, err
}

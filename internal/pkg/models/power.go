package models

import (
	"MySportWeb/internal/pkg/types"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PowerZones is the number of power zones (activityService.PowerZoneLimits): Coggan's 7
const PowerZones = 7

// FTPSport is the sport of the FTP and of the power metrics: the power of a run or a row isn't the one of
// a ride
const FTPSport = "cycling"

// BestPower is the best power of a user over a duration, and the activity it is from
type BestPower struct {
	ActivityID uuid.UUID
	Date       time.Time
	Value      float64 // W
}

// GetBestPower returns the highest power of the rides of a user over duration seconds, between from and to
// (included), nil without any
func (db *DB) GetBestPower(userID uint, duration float64, from, to time.Time) (*BestPower, error) {
	var best []BestPower
	err := db.Table("activities AS a").
		Select("a.id AS activity_id, a.date, e.value").
		Joins(`CROSS JOIN LATERAL jsonb_to_recordset(CASE WHEN jsonb_typeof(a.best_efforts) = 'array'
			THEN a.best_efforts ELSE '[]'::jsonb END) AS e(sport text, kind text, target float8, value float8)`).
		Where("a.user_id = ? AND a.date >= ? AND a.date <= ? AND e.sport = ? AND e.kind = ? AND e.target = ?",
			userID, from, to, FTPSport, types.EffortPower, duration).
		Order("e.value DESC, a.date").Limit(1).Scan(&best).Error
	if err != nil || len(best) == 0 {
		return nil, err
	}
	return &best[0], nil
}

// GetUserFTP returns the FTP a user set, 0 when estimated
func (db *DB) GetUserFTP(userID uint) (uint16, error) {
	var ftps []uint16
	err := db.Model(&Users{}).Where("id = ?", userID).Pluck("ftp", &ftps).Error
	if err != nil || len(ftps) == 0 {
		return 0, err
	}
	return ftps[0], nil
}

// GetActivityPower returns what the power metrics of an activity are computed from: its date, sport, user,
// measured power, best efforts, and its training score with its source and heart rates
func (db *DB) GetActivityPower(id uuid.UUID) (Activity, error) {
	var activity Activity
	err := db.Select("id", "user_id", "date", "sport", "file_path", "trim_end", "powers", "power_ts",
		"best_efforts", "trimp", "score_source", "hearts", "heart_time_stamps").Preload("User").
		First(&activity, "id = ?", id).Error
	return activity, err
}

// GetActivitiesWithoutPowerZones returns the ids of the activities whose power metrics were never computed
// (NULL, or a nil array saved as JSON null), oldest first: an FTP estimated from the rides before them
func (db *DB) GetActivitiesWithoutPowerZones() ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("power_zones IS NULL OR power_zones = 'null'").Order("date asc").
		Pluck("id", &ids).Error
	return ids, err
}

// SetActivityPowerMetrics saves the power metrics of an activity, and its training score with its source
func (db *DB) SetActivityPowerMetrics(a *Activity) error {
	updates := map[string]interface{}{
		"power_zones": a.PowerZones, "power_zone_bounds": a.PowerZoneBounds, "ftp": a.FTP,
		"normalized_power": a.NormalizedPower, "intensity": a.Intensity, "tss": a.TSS,
		"score_source": a.ScoreSource,
	}
	if a.Trimp != nil {
		updates["trimp"] = *a.Trimp
	}
	return db.Model(&Activity{}).Where("id = ?", a.ID).Updates(updates).Error
}

// ResetPowerMetrics forgets the power metrics of the rides of a user, to compute them again: their FTP changed
func (db *DB) ResetPowerMetrics(userID uint) error {
	return db.Model(&Activity{}).Where("user_id = ? AND sport = ?", userID, FTPSport).
		Updates(map[string]interface{}{"power_zones": nil}).Error
}

// ResetPowerMetricsBelow forgets the power metrics of the rides of a user between from and to (included)
// computed with an FTP below ftp: a new ride may raise their estimated FTP to ftp
func (db *DB) ResetPowerMetricsBelow(userID uint, from, to time.Time, ftp uint16) (int64, error) {
	return db.resetPowerMetrics(db.Where("ftp < ?", ftp), userID, from, to)
}

// ResetPowerMetricsAt forgets the power metrics of the rides of a user between from and to (included)
// computed with the FTP ftp: the ride it came from is deleted
func (db *DB) ResetPowerMetricsAt(userID uint, from, to time.Time, ftp uint16) (int64, error) {
	return db.resetPowerMetrics(db.Where("ftp = ?", ftp), userID, from, to)
}

func (db *DB) resetPowerMetrics(ftpCondition *gorm.DB, userID uint, from, to time.Time) (int64, error) {
	result := db.Model(&Activity{}).
		Where("user_id = ? AND sport = ? AND date >= ? AND date <= ? AND power_zones IS NOT NULL",
			userID, FTPSport, from, to).
		Where(ftpCondition).
		Updates(map[string]interface{}{"power_zones": nil})
	return result.RowsAffected, result.Error
}

// GetWeeklyPowerZones returns the seconds in each power zone of the last weeks of a user, as GetWeeklyHrZones
func (db *DB) GetWeeklyPowerZones(userID uint, now time.Time, weeks int) ([]types.WeeklyHrZones, error) {
	current := weekStart(now)
	first := current.AddDate(0, 0, -7*(weeks-1))
	result := make([]types.WeeklyHrZones, weeks)
	index := map[string]int{}
	for i := range result {
		week := first.AddDate(0, 0, 7*i).Format("2006-01-02")
		result[i] = types.WeeklyHrZones{Week: week, Zones: make([]float64, PowerZones)}
		index[week] = i
	}
	var activities []struct {
		Date       time.Time
		PowerZones types.FloatArray
	}
	err := db.Model(&Activity{}).Select("date", "power_zones").
		Where("user_id = ? AND date >= ? AND power_zones IS NOT NULL AND power_zones <> 'null'", userID, first).
		Scan(&activities).Error
	if err != nil {
		return nil, err
	}
	for _, activity := range activities {
		i, ok := index[weekStart(activity.Date.In(now.Location())).Format("2006-01-02")]
		if !ok {
			continue
		}
		for zone, seconds := range activity.PowerZones {
			if zone < PowerZones {
				result[i].Zones[zone] += seconds
			}
		}
	}
	return result, nil
}

// GetActivitiesWithoutPowerCurve returns the ids of the activities whose power curve was never computed
func (db *DB) GetActivitiesWithoutPowerCurve() ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("power_curve IS NULL OR power_curve = 'null'::jsonb").Order("date asc").
		Pluck("id", &ids).Error
	return ids, err
}

// SetActivityPowerCurve saves the power curve of an activity
func (db *DB) SetActivityPowerCurve(id uuid.UUID, curve types.PowerCurve) error {
	return db.Model(&Activity{}).Where("id = ?", id).Update("power_curve", curve).Error
}

// PowerCurveBest is the best power of a user over a duration in a period, and the ride it comes from
type PowerCurveBest struct {
	Duration      float64   `json:"duration"`
	Watts         float64   `json:"watts"`
	StartOffset   float64   `json:"start_offset"`
	ActivityID    uuid.UUID `json:"activity_id"`
	ActivityTitle string    `json:"activity_title"`
	Date          time.Time `json:"date"`
}

// GetPowerCurve returns the best power of the rides of a user over each duration, between from (included)
// and to (excluded), nil for no bound; by increasing duration, the oldest ride on a tie
func (db *DB) GetPowerCurve(userID uint, from, to *time.Time) ([]PowerCurveBest, error) {
	best := []PowerCurveBest{}
	query := db.Table("activities AS a").
		Select(`DISTINCT ON (p.duration) p.duration, p.watts, p.start_offset, a.id AS activity_id,
			a.title AS activity_title, a.date`).
		// jsonb_to_recordset fails on the JSON null of the rides not computed yet
		Joins(`CROSS JOIN LATERAL jsonb_to_recordset(CASE WHEN jsonb_typeof(a.power_curve) = 'array'
			THEN a.power_curve ELSE '[]'::jsonb END) AS p(duration float8, watts float8, start_offset float8)`).
		Where("a.user_id = ? AND a.sport = ?", userID, FTPSport)
	if from != nil {
		query = query.Where("a.date >= ?", *from)
	}
	if to != nil {
		query = query.Where("a.date < ?", *to)
	}
	err := query.Order("p.duration, p.watts DESC, a.date").Scan(&best).Error
	return best, err
}

// GetActivityPowerStream returns the measured power of an activity, its sport and its user
func (db *DB) GetActivityPowerStream(id uuid.UUID) (Activity, error) {
	var activity Activity
	err := db.Select("id", "user_id", "sport", "powers", "power_ts").First(&activity, "id = ?", id).Error
	return activity, err
}

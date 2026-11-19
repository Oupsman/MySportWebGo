package models

import (
	"MySportWeb/internal/pkg/types"
	"time"

	"github.com/google/uuid"
)

// MaxRecordsRank is the most efforts per target GetRecords returns
const MaxRecordsRank = 10

// GetActivitiesWithoutBestEfforts returns the ids of the activities whose best efforts were never computed
// (NULL, or a nil array saved as JSON null), oldest first
func (db *DB) GetActivitiesWithoutBestEfforts() ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("best_efforts IS NULL OR best_efforts = 'null'::jsonb").Order("date asc").
		Pluck("id", &ids).Error
	return ids, err
}

// GetActivityDistances returns an activity with only what its best efforts need: its user, FIT file and trim,
// and to compute them without the file, its sport, legs and distance series
func (db *DB) GetActivityDistances(id uuid.UUID) (Activity, error) {
	var activity Activity
	err := db.Select("id", "user_id", "file_path", "trim_end", "sport", "legs", "distances", "time_stamps").
		Preload("User").First(&activity, "id = ?", id).Error
	return activity, err
}

// SetActivityBestEfforts saves the best efforts of an activity
func (db *DB) SetActivityBestEfforts(id uuid.UUID, efforts types.BestEffortArray) error {
	if efforts == nil {
		efforts = types.BestEffortArray{}
	}
	return db.Model(&Activity{}).Where("id = ?", id).Update("best_efforts", efforts).Error
}

// GetRecords returns the best efforts of a user, the top ones (1 to MaxRecordsRank) of each sport, kind and
// target, ranked: the shortest times over a distance, the highest powers over a duration, the oldest first
// on a tie. sport "" for every sport; since, when not nil, ignores the activities before it.
func (db *DB) GetRecords(userID uint, sport string, since *time.Time, top int) ([]types.EffortRecord, error) {
	records := []types.EffortRecord{}
	query := db.Table("activities AS a").
		Select(`a.id AS activity_id, a.title AS activity_title, a.date, e.sport, e.kind, e.target, e.value,
			e.start_offset, ROW_NUMBER() OVER (PARTITION BY e.sport, e.kind, e.target
				ORDER BY CASE WHEN e.kind = ? THEN e.value ELSE -e.value END, a.date) AS rank`, types.EffortDistance).
		// jsonb_to_recordset fails on the JSON null of the activities not computed yet
		Joins(`CROSS JOIN LATERAL jsonb_to_recordset(CASE WHEN jsonb_typeof(a.best_efforts) = 'array'
			THEN a.best_efforts ELSE '[]'::jsonb END)
			AS e(sport text, kind text, target float8, value float8, start_offset float8)`).
		Where("a.user_id = ?", userID)
	if sport != "" {
		query = query.Where("e.sport = ?", sport)
	}
	if since != nil {
		query = query.Where("a.date >= ?", *since)
	}
	err := db.Table("(?) AS ranked", query).Where("rank <= ?", top).
		Order("sport, kind, target, rank").Scan(&records).Error
	return records, err
}

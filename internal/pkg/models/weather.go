package models

import (
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/weather"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SetActivityWeather saves the weather at the start of an activity
func (db *DB) SetActivityWeather(id uuid.UUID, w weather.Weather) error {
	value, err := json.Marshal(w)
	if err != nil {
		return err
	}
	// not through the model: the field is read only there (a save of the activity never erases it), and
	// gorm ignores the updates of a read only field
	return db.Exec("UPDATE activities SET weather = ?::jsonb WHERE id = ?", string(value), id).Error
}

// WeatherToFetch is an activity whose weather isn't known yet: when and where it started
type WeatherToFetch struct {
	ID            uuid.UUID
	Date          time.Time
	StartPosition types.GpsPoint
}

// GetActivitiesWithoutWeather returns the activities with a start position and no weather yet, the most
// recent first, at most limit
func (db *DB) GetActivitiesWithoutWeather(limit int) ([]WeatherToFetch, error) {
	var activities []WeatherToFetch
	err := db.Table("activities").Select("id, date, start_position").
		Where("weather IS NULL AND start_position IS NOT NULL").Order("date DESC").Limit(limit).
		Scan(&activities).Error
	return activities, err
}

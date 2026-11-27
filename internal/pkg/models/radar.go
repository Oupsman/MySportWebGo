package models

import (
	"MySportWeb/internal/pkg/types"

	"github.com/google/uuid"
)

// GetActivitiesWithoutRadarEvents returns the activities whose radar events were never computed, the oldest
// first
func (db *DB) GetActivitiesWithoutRadarEvents() ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("radar_events IS NULL OR radar_events = 'null'::jsonb").Order("date asc").
		Pluck("id", &ids).Error
	return ids, err
}

// GetActivitiesWithoutRadarConnected returns the activities for which whether a radar was connected was never
// computed, the oldest first
func (db *DB) GetActivitiesWithoutRadarConnected() ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Model(&Activity{}).Where("radar_connected IS NULL").Order("date asc").Pluck("id", &ids).Error
	return ids, err
}

// SetActivityRadarConnected saves whether a radar was connected during an activity
func (db *DB) SetActivityRadarConnected(id uuid.UUID, connected bool) error {
	return db.Model(&Activity{}).Where("id = ?", id).Update("radar_connected", connected).Error
}

// SetActivityRadarEvents saves the radar events of an activity, nil as none
func (db *DB) SetActivityRadarEvents(id uuid.UUID, events types.RadarEventArray) error {
	if events == nil {
		events = types.RadarEventArray{}
	}
	return db.Model(&Activity{}).Where("id = ?", id).Update("radar_events", events).Error
}

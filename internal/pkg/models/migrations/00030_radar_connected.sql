-- +goose Up
-- whether a rear radar was connected during the activity, NULL until the backfill reads its file; an activity
-- without file had none
ALTER TABLE activities ADD COLUMN radar_connected boolean;
UPDATE activities SET radar_connected = false WHERE file_path IS NULL OR file_path = '';

-- the vehicles of the radar drawn on the maps, the approaching ones and the fast ones apart: as the user set
-- both of them
ALTER TABLE users ADD COLUMN show_radar_approaching boolean NOT NULL DEFAULT true;
ALTER TABLE users ADD COLUMN show_radar_fast boolean NOT NULL DEFAULT true;
UPDATE users SET show_radar_approaching = show_radar_events, show_radar_fast = show_radar_events;
ALTER TABLE users DROP COLUMN show_radar_events;

-- +goose Down
ALTER TABLE users ADD COLUMN show_radar_events boolean NOT NULL DEFAULT true;
UPDATE users SET show_radar_events = show_radar_approaching OR show_radar_fast;
ALTER TABLE users DROP COLUMN show_radar_fast;
ALTER TABLE users DROP COLUMN show_radar_approaching;
ALTER TABLE activities DROP COLUMN radar_connected;

-- +goose Up
-- the vehicles a rear radar saw behind the rider, recorded by EIradar on a Karoo: NULL until computed. Only the
-- files of a Hammerhead device can have them: the others have none, the backfill reads the Hammerhead ones
ALTER TABLE activities ADD COLUMN radar_events jsonb;
UPDATE activities SET radar_events = '[]'::jsonb WHERE device_name IS NULL OR device_name NOT ILIKE 'hammerhead%';
-- whether they are drawn on the maps of the activities of the user
ALTER TABLE users ADD COLUMN show_radar_events boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE users DROP COLUMN show_radar_events;
ALTER TABLE activities DROP COLUMN radar_events;

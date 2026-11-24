-- +goose Up
-- the weather at the start of an activity (weather.Weather as JSON): NULL until asked, {"unavailable": true}
-- when the source has none for that hour and place
ALTER TABLE activities ADD COLUMN weather jsonb;

-- +goose Down
ALTER TABLE activities DROP COLUMN weather;

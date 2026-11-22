-- +goose Up
-- the best power of a ride with a power meter over durations from 1 s to 5 h: [{duration, watts,
-- start_offset}], empty without power, NULL until computed
ALTER TABLE activities ADD COLUMN power_curve jsonb;

-- +goose Down
ALTER TABLE activities DROP COLUMN power_curve;

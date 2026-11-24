-- +goose Up
-- the time spent moving (seconds) and the average speed while moving (m/s), from the records: a device
-- without auto-pause counts the stops in its duration. NULL until computed, 0 when the records don't tell.
ALTER TABLE activities ADD COLUMN moving_time double precision;
ALTER TABLE activities ADD COLUMN avg_moving_speed double precision;

-- +goose Down
ALTER TABLE activities DROP COLUMN avg_moving_speed;
ALTER TABLE activities DROP COLUMN moving_time;

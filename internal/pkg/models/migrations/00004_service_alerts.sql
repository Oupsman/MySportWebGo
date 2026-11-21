-- +goose Up
-- the service cycle of an equipment whose "service due" notification was sent: once per cycle. The cycle is
-- the date of the latest service, none (service_alerted true, service_alerted_for NULL) before the first one.
ALTER TABLE equipments ADD COLUMN service_alerted boolean NOT NULL DEFAULT false;
ALTER TABLE equipments ADD COLUMN service_alerted_for timestamp with time zone;

-- +goose Down
ALTER TABLE equipments DROP COLUMN service_alerted_for;
ALTER TABLE equipments DROP COLUMN service_alerted;

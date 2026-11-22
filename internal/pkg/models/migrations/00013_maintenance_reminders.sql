-- +goose Up
-- the maintenance reminders of an equipment, several each: due after every_km km, every_hours hours of
-- activities, or every_count every_unit (calendar days, weeks, months, years) since they were last done,
-- whichever comes first. 0 (or '') is no such interval, one at least. A time interval with only_when_used
-- waits for an activity with the equipment since it was last done. Until the first time, their cycle starts
-- at starts_at. notified, notified_for: the cycle (the date of the latest service done for them, NULL before
-- the first one) whose notification was sent, once per cycle.
CREATE TABLE maintenance_reminders (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    equipment_id uuid NOT NULL REFERENCES equipments (id) ON DELETE CASCADE,
    name text NOT NULL,
    every_km integer NOT NULL DEFAULT 0 CHECK (every_km >= 0),
    every_hours integer NOT NULL DEFAULT 0 CHECK (every_hours >= 0),
    every_count integer NOT NULL DEFAULT 0 CHECK (every_count >= 0),
    every_unit text NOT NULL DEFAULT '' CHECK (every_unit IN ('', 'day', 'week', 'month', 'year')),
    only_when_used boolean NOT NULL DEFAULT true,
    starts_at timestamp with time zone NOT NULL DEFAULT now(),
    notified boolean NOT NULL DEFAULT false,
    notified_for timestamp with time zone,
    CHECK (every_km > 0 OR every_hours > 0 OR (every_count > 0 AND every_unit <> ''))
);
CREATE INDEX maintenance_reminders_equipment_id ON maintenance_reminders (equipment_id);

-- the reminders a service did: it starts their next cycle
CREATE TABLE service_reminders (
    service_id uuid NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    reminder_id uuid NOT NULL REFERENCES maintenance_reminders (id) ON DELETE CASCADE,
    PRIMARY KEY (service_id, reminder_id)
);
CREATE INDEX service_reminders_reminder_id ON service_reminders (reminder_id);

-- the maintenance interval of an equipment becomes its "Service" reminder: its km counted since the first
-- activity (starts_at at the epoch) until a service, every service of the equipment done for it, its
-- notification state kept (no notification again at the upgrade)
INSERT INTO maintenance_reminders (equipment_id, name, every_km, starts_at, notified, notified_for)
SELECT id, 'Service', maintenance_interval, '1970-01-01T00:00:00Z', service_alerted, service_alerted_for
FROM equipments WHERE maintenance_interval > 0;
INSERT INTO service_reminders (service_id, reminder_id)
SELECT s.id, r.id FROM services s JOIN maintenance_reminders r ON r.equipment_id = s.equipment_id;

ALTER TABLE equipments DROP COLUMN maintenance_interval;
ALTER TABLE equipments DROP COLUMN service_alerted;
ALTER TABLE equipments DROP COLUMN service_alerted_for;

-- +goose Down
ALTER TABLE equipments ADD COLUMN maintenance_interval bigint;
ALTER TABLE equipments ADD COLUMN service_alerted boolean NOT NULL DEFAULT false;
ALTER TABLE equipments ADD COLUMN service_alerted_for timestamp with time zone;
-- the shortest km interval of each equipment, its notification state
UPDATE equipments e SET maintenance_interval = r.every_km, service_alerted = r.notified,
    service_alerted_for = r.notified_for
FROM (SELECT DISTINCT ON (equipment_id) equipment_id, every_km, notified, notified_for
      FROM maintenance_reminders WHERE every_km > 0 ORDER BY equipment_id, every_km) r
WHERE r.equipment_id = e.id;
DROP TABLE service_reminders;
DROP TABLE maintenance_reminders;

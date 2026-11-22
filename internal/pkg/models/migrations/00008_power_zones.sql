-- +goose Up
-- the functional threshold power of a user (W), 0: estimated from their rides
ALTER TABLE users ADD COLUMN ftp integer NOT NULL DEFAULT 0;

-- the power metrics of the rides with a power meter: the time in the power zones and the watts bounding
-- them, the FTP they were computed with, the normalized power, the intensity factor and the TSS
ALTER TABLE activities ADD COLUMN power_zones bytea;
ALTER TABLE activities ADD COLUMN power_zone_bounds bytea;
ALTER TABLE activities ADD COLUMN ftp integer NOT NULL DEFAULT 0;
ALTER TABLE activities ADD COLUMN normalized_power numeric;
ALTER TABLE activities ADD COLUMN intensity numeric;
ALTER TABLE activities ADD COLUMN tss numeric;
-- where the training score (trimp) comes from: device, power, hr; empty before it was recorded
ALTER TABLE activities ADD COLUMN score_source text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE activities DROP COLUMN score_source;
ALTER TABLE activities DROP COLUMN tss;
ALTER TABLE activities DROP COLUMN intensity;
ALTER TABLE activities DROP COLUMN normalized_power;
ALTER TABLE activities DROP COLUMN ftp;
ALTER TABLE activities DROP COLUMN power_zone_bounds;
ALTER TABLE activities DROP COLUMN power_zones;
ALTER TABLE users DROP COLUMN ftp;

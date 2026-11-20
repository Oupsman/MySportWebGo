-- +goose Up
-- the version of the drawing of the thumbnail of each activity: 0, the ones drawn by Chromium, without the
-- attribution of the map data, are drawn again at the start
ALTER TABLE activities ADD COLUMN thumbnail_version bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE activities DROP COLUMN thumbnail_version;

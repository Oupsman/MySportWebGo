-- +goose Up
-- the file an activity was uploaded as when it wasn't a FIT file (GPX, TCX): kept beside the FIT file it was
-- converted to (file_path), to convert it again with a later version. Not served.
ALTER TABLE activities ADD COLUMN source_file_path text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE activities DROP COLUMN source_file_path;

-- +goose Up
-- the columns of the activities table of the user, chosen and ordered by them: a JSON array of column ids,
-- NULL for the default columns
ALTER TABLE users ADD COLUMN activities_columns jsonb;

-- +goose Down
ALTER TABLE users DROP COLUMN activities_columns;

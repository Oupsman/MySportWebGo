-- +goose Up
-- the widgets of the dashboard of the user, chosen and arranged by them: a JSON array of
-- {"id", "w", "h", "hidden"} in their order, NULL for the default layout
ALTER TABLE users ADD COLUMN dashboard_layout jsonb;

-- +goose Down
ALTER TABLE users DROP COLUMN dashboard_layout;

-- +goose Up
-- the name the user gives a device ("Edge 530 of the gravel bike"), shown in place of the one its files
-- give (name, kept: it is how the device is told apart from its aliases); empty for the files' name
ALTER TABLE devices ADD COLUMN label text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE devices DROP COLUMN label;

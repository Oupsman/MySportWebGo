-- +goose Up
-- the storage quota an admin gave a user, in bytes: NULL for the one of the instance (USER_QUOTA), 0 for none
ALTER TABLE users ADD COLUMN quota_bytes bigint CHECK (quota_bytes >= 0);

-- +goose Down
ALTER TABLE users DROP COLUMN quota_bytes;

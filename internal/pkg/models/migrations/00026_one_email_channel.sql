-- +goose Up
-- a user has one email channel: turned on twice at the same time, two were created and each notification
-- went twice. The soft deleted ones first, then the duplicates but the oldest, with their notifications.
DELETE FROM notifications WHERE channel_id IN (
    SELECT id FROM channels WHERE channel_provider = 'email' AND deleted_at IS NOT NULL);
DELETE FROM channels WHERE channel_provider = 'email' AND deleted_at IS NOT NULL;
DELETE FROM notifications WHERE channel_id IN (
    SELECT c.id FROM channels c WHERE c.channel_provider = 'email' AND EXISTS (
        SELECT 1 FROM channels o WHERE o.user_id = c.user_id AND o.channel_provider = 'email' AND o.id < c.id));
DELETE FROM channels c WHERE c.channel_provider = 'email' AND EXISTS (
    SELECT 1 FROM channels o WHERE o.user_id = c.user_id AND o.channel_provider = 'email' AND o.id < c.id);
CREATE UNIQUE INDEX channels_one_email ON channels (user_id) WHERE channel_provider = 'email';

-- +goose Down
DROP INDEX channels_one_email;

-- +goose Up
-- what the users do, for the admins: logins, uploads, deletions, the changes of the admins on an account...
-- (kind, see models.EventLogin...), kept EventRetention. user_id is null once the user is deleted: username
-- keeps who it was. detail never holds a secret.
CREATE TABLE user_events (
    id bigserial PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    user_id bigint REFERENCES users (id) ON DELETE SET NULL,
    username text NOT NULL DEFAULT '',
    kind text NOT NULL,
    detail text NOT NULL DEFAULT '',
    ip text NOT NULL DEFAULT ''
);
CREATE INDEX user_events_created_at ON user_events (created_at);
CREATE INDEX user_events_user_id ON user_events (user_id, id);
CREATE INDEX user_events_kind ON user_events (kind, id);

-- what a notification is about (goal, comment...), and when it was sent: shown to the admins
ALTER TABLE notifications ADD COLUMN kind text NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN sent_at timestamp with time zone;
UPDATE notifications SET sent_at = updated_at WHERE sent;
CREATE INDEX notifications_created_at ON notifications (created_at);

-- +goose Down
DROP INDEX notifications_created_at;
ALTER TABLE notifications DROP COLUMN sent_at;
ALTER TABLE notifications DROP COLUMN kind;
DROP TABLE user_events;

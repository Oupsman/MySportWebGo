-- +goose Up
-- who follows whom: accepted_at NULL is a request the followee hasn't accepted yet
CREATE TABLE follows (
    follower_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    followee_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    accepted_at timestamp with time zone,
    PRIMARY KEY (follower_id, followee_id),
    CHECK (follower_id <> followee_id)
);
CREATE INDEX follows_followee_id ON follows (followee_id);

-- whether a user accepts each follower (true), or anyone follows them at once
ALTER TABLE users ADD COLUMN follow_approval boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE users DROP COLUMN follow_approval;
DROP TABLE follows;

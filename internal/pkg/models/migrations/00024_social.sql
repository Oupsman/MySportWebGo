-- +goose Up
-- the cheers of the users on the activities they may see, one per user and activity; notified_at is when its
-- owner was told (CheckCheers batches them), NULL until then
CREATE TABLE cheers (
    activity_id uuid NOT NULL REFERENCES activities (id) ON DELETE CASCADE,
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    notified_at timestamp with time zone,
    PRIMARY KEY (activity_id, user_id)
);
CREATE INDEX cheers_user_id ON cheers (user_id);
CREATE INDEX cheers_unnotified ON cheers (created_at) WHERE notified_at IS NULL;

-- the comments of the users on the activities they may see
CREATE TABLE comments (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    activity_id uuid NOT NULL REFERENCES activities (id) ON DELETE CASCADE,
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);
CREATE INDEX comments_activity_id ON comments (activity_id, created_at);
CREATE INDEX comments_user_id ON comments (user_id);

-- whether a user is notified of the cheers and comments on their activities
ALTER TABLE users ADD COLUMN notify_social boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE users DROP COLUMN notify_social;
DROP TABLE comments;
DROP TABLE cheers;

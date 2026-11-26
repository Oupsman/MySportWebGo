-- +goose Up
-- the places where the users work: a circle of diameter meters around lat, lon. An activity imported that
-- starts or ends in one is a commute.
CREATE TABLE workplaces (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL DEFAULT '',
    lat double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lon double precision NOT NULL CHECK (lon BETWEEN -180 AND 180),
    diameter double precision NOT NULL CHECK (diameter > 0)
);
CREATE INDEX workplaces_user_id ON workplaces (user_id);

-- +goose Down
DROP TABLE workplaces;

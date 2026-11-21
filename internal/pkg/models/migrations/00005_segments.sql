-- +goose Up
-- the segments: stretches of a ride a user selected, to compare their passes on them
CREATE TABLE segments (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL,
    sport text NOT NULL,
    -- the trace, JSON [{lat, lon}] as the traces of the activities
    points bytea NOT NULL,
    distance double precision NOT NULL,
    elevation_gain double precision NOT NULL,
    -- the box around the trace: the activities that can pass on it
    min_lat double precision NOT NULL,
    min_lon double precision NOT NULL,
    max_lat double precision NOT NULL,
    max_lon double precision NOT NULL,
    source_activity_id uuid REFERENCES activities (id) ON DELETE SET NULL
);
CREATE INDEX segments_user_id ON segments (user_id);

-- the passes of the activities on the segments: an activity passing twice (laps) has two
CREATE TABLE segment_efforts (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    segment_id uuid NOT NULL REFERENCES segments (id) ON DELETE CASCADE,
    activity_id uuid NOT NULL REFERENCES activities (id) ON DELETE CASCADE,
    start_offset double precision NOT NULL,
    elapsed double precision NOT NULL,
    avg_heart_rate double precision,
    avg_power double precision,
    UNIQUE (segment_id, activity_id, start_offset)
);
CREATE INDEX segment_efforts_activity_id ON segment_efforts (activity_id);

-- +goose Down
DROP TABLE segment_efforts;
DROP TABLE segments;

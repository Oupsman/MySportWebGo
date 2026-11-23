-- +goose Up
-- the training goals of the users: a target of a metric (distance and elevation in meters, duration in
-- seconds, activities a count) over a period, recurring (each week, month or year) or custom (starts_on to
-- ends_on, included), counting the activities of sports (a JSON array of sport names, empty for all of them).
-- Their progress is computed from the activities. notified_for: the first day of the period whose
-- "reached" notification was queued, once per period.
CREATE TABLE goals (
    id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL DEFAULT '',
    metric text NOT NULL CHECK (metric IN ('distance', 'duration', 'elevation', 'activities')),
    target double precision NOT NULL CHECK (target > 0),
    sports jsonb NOT NULL DEFAULT '[]',
    period text NOT NULL CHECK (period IN ('week', 'month', 'year', 'custom')),
    starts_on date,
    ends_on date,
    notified_for date,
    CHECK ((period = 'custom') = (starts_on IS NOT NULL AND ends_on IS NOT NULL)),
    CHECK (ends_on >= starts_on)
);
CREATE INDEX goals_user_id ON goals (user_id);

-- +goose Down
DROP TABLE goals;

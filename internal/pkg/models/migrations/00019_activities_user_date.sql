-- +goose Up
-- the activities of a user by date: the dashboard, the lists, the calendar, the goals and the highlights read
-- them so, and read the whole table without (the activities of every user)
CREATE INDEX activities_user_date ON activities (user_id, date);

-- +goose Down
DROP INDEX activities_user_date;

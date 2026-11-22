-- +goose Up
-- the power curves and power metrics were computed from the power estimated on the rides without power
-- meter too, and the curves of the other sports were missing: computed again, from the measured power only
UPDATE activities SET power_curve = NULL;
UPDATE activities SET power_zones = NULL WHERE sport = 'cycling';

-- +goose Down
-- nothing to undo: the version before computes them again as it did
SELECT 1;

-- +goose Up
-- the default equipment of each sport of a user, given to the activities of that sport at their import: one
-- per sport, an equipment can be the default one of several sports (shoes for running and hiking)
CREATE TABLE equipment_defaults (
    user_id bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    sport text NOT NULL,
    equipment_id uuid NOT NULL REFERENCES equipments (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, sport)
);
CREATE INDEX equipment_defaults_equipment_id ON equipment_defaults (equipment_id);

-- the default equipment of a user becomes the default one of the sport of most of its activities, of
-- cycling without activity (the only sport its weight counts for). A user with several defaults (an old
-- bug) keeps the latest updated one for a sport.
INSERT INTO equipment_defaults (user_id, sport, equipment_id)
SELECT DISTINCT ON (e.user_id, s.sport) e.user_id, s.sport, e.id
FROM equipments e
CROSS JOIN LATERAL (
    SELECT COALESCE((SELECT a.sport FROM activities a WHERE a.equipment_id = e.id
                     GROUP BY a.sport ORDER BY COUNT(*) DESC, a.sport LIMIT 1), 'cycling') AS sport
) s
WHERE e.is_default AND e.user_id IS NOT NULL AND e.deleted_at IS NULL
ORDER BY e.user_id, s.sport, e.updated_at DESC NULLS LAST, e.id;

ALTER TABLE equipments DROP COLUMN is_default;

-- +goose Down
ALTER TABLE equipments ADD COLUMN is_default boolean;
-- one default per user: the cycling one, else the one of the first sport
UPDATE equipments SET is_default = true WHERE id IN (
    SELECT DISTINCT ON (user_id) equipment_id FROM equipment_defaults
    ORDER BY user_id, sport <> 'cycling', sport
);
DROP TABLE equipment_defaults;

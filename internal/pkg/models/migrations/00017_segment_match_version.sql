-- +goose Up
-- the version of the matching rules the passes of a segment were found with (SegmentMatchVersion of
-- activityService): the segments of an older version are matched again at the start. The existing ones
-- start at 0, matched again once with the rules of this release.
ALTER TABLE segments ADD COLUMN match_version integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE segments DROP COLUMN match_version;

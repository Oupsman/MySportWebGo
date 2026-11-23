package models

import (
	"MySportWeb/internal/pkg/types"
	"slices"
	"time"

	"github.com/google/uuid"
)

// HighlightTop is the lowest rank a best effort or a segment pass is a highlight with: the podium
const HighlightTop = 3

// highlightRow is a best effort or a segment pass of an activity, compared with those of the other activities
// of the user: how many came before it, how many of those were as good or better, the best of them, and how
// many better ones came after it
type highlightRow struct {
	types.Highlight
	Earlier      int
	BetterBefore int
	BetterAfter  int
}

// highlight keeps a row ranked in the podium at its date, after other ones: the first effort or pass is
// best of nothing
func (r highlightRow) highlight() (types.Highlight, bool) {
	if r.Earlier == 0 || r.BetterBefore >= HighlightTop {
		return types.Highlight{}, false
	}
	h := r.Highlight
	h.Rank = r.BetterBefore + 1
	h.Current = h.Rank == 1 && r.BetterAfter == 0
	return h, true
}

// effortsOf expands the best efforts of the activities of a user, one row each
const effortsOf = `SELECT a.id, a.title, a.date, x.sport, x.kind, x.target, x.value, x.start_offset
	FROM activities a CROSS JOIN LATERAL jsonb_to_recordset(CASE WHEN jsonb_typeof(a.best_efforts) = 'array'
		THEN a.best_efforts ELSE '[]'::jsonb END)
		AS x(sport text, kind text, target float8, value float8, start_offset float8)
	WHERE a.user_id = ?`

// effortHighlights returns the best efforts of the activities ranked in the podium of their distance or
// duration at their date. A shorter time is better over a distance, a higher power over a duration; on a tie
// the older one stays ahead.
func (db *DB) effortHighlights(userID uint, ids []uuid.UUID) ([]types.Highlight, error) {
	var rows []highlightRow
	err := db.Raw(`WITH e AS (`+effortsOf+`)
		SELECT m.id AS activity_id, m.title AS activity_title, m.date, m.sport, m.kind AS effort_kind, m.target,
			m.value, m.start_offset,
			count(o.id) FILTER (WHERE o.date < m.date) AS earlier,
			count(o.id) FILTER (WHERE o.date < m.date AND CASE WHEN m.kind = ? THEN o.value <= m.value
				ELSE o.value >= m.value END) AS better_before,
			count(o.id) FILTER (WHERE o.date > m.date AND CASE WHEN m.kind = ? THEN o.value < m.value
				ELSE o.value > m.value END) AS better_after,
			CASE WHEN m.kind = ? THEN min(o.value) FILTER (WHERE o.date < m.date)
				ELSE max(o.value) FILTER (WHERE o.date < m.date) END AS previous
		FROM e m LEFT JOIN e o ON o.sport = m.sport AND o.kind = m.kind AND o.target = m.target AND o.id <> m.id
		WHERE m.id IN ?
		GROUP BY m.id, m.title, m.date, m.sport, m.kind, m.target, m.value, m.start_offset`,
		userID, types.EffortDistance, types.EffortDistance, types.EffortDistance, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	highlights := []types.Highlight{}
	for _, row := range rows {
		row.Kind = types.HighlightEffort
		if h, ok := row.highlight(); ok {
			highlights = append(highlights, h)
		}
	}
	return highlights, nil
}

// segmentHighlights returns the passes of the activities ranked in the podium of their segment at their date
func (db *DB) segmentHighlights(userID uint, ids []uuid.UUID) ([]types.Highlight, error) {
	var rows []highlightRow
	err := db.Raw(`SELECT e.activity_id, a.title AS activity_title, a.date, s.sport, e.segment_id, s.name,
			e.elapsed AS value, e.start_offset,
			count(o.id) FILTER (WHERE b.date < a.date) AS earlier,
			count(o.id) FILTER (WHERE b.date < a.date AND o.elapsed <= e.elapsed) AS better_before,
			count(o.id) FILTER (WHERE b.date > a.date AND o.elapsed < e.elapsed) AS better_after,
			min(o.elapsed) FILTER (WHERE b.date < a.date) AS previous
		FROM segment_efforts e
		JOIN activities a ON a.id = e.activity_id
		JOIN segments s ON s.id = e.segment_id
		LEFT JOIN (segment_efforts o JOIN activities b ON b.id = o.activity_id)
			ON o.segment_id = e.segment_id AND b.id <> a.id
		WHERE e.activity_id IN ? AND s.user_id = ?
		GROUP BY e.id, e.activity_id, a.title, a.date, s.sport, e.segment_id, s.name, e.elapsed, e.start_offset`,
		ids, userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	highlights := []types.Highlight{}
	for _, row := range rows {
		row.Kind = types.HighlightSegment
		if h, ok := row.highlight(); ok {
			highlights = append(highlights, h)
		}
	}
	return highlights, nil
}

// goalRunning is the running value of the metric of a goal over the activities of a period, in their order
var goalRunning = map[string]string{
	"distance":   "sum(coalesce(distance, 0)) OVER (ORDER BY date, id)",
	"duration":   "sum(coalesce(duration, 0)) OVER (ORDER BY date, id)",
	"elevation":  "sum(coalesce(positive_elevation, 0)) OVER (ORDER BY date, id)",
	"activities": "count(*) OVER (ORDER BY date, id)",
}

// highlightActivity is an activity whose highlights are computed
type highlightActivity struct {
	ID    uuid.UUID
	Title string
	Date  time.Time
	Sport string
}

// goalHighlights returns the goals the activities completed: the activity that took the metric of the goal
// to its target in its period (in loc), at most one per goal and period
func (db *DB) goalHighlights(userID uint, activities []highlightActivity, loc *time.Location) ([]types.Highlight, error) {
	var goals []Goal
	if err := db.Where("user_id = ?", userID).Find(&goals).Error; err != nil {
		return nil, err
	}
	wanted := map[uuid.UUID]bool{}
	for _, a := range activities {
		wanted[a.ID] = true
	}
	highlights := []types.Highlight{}
	for _, g := range goals {
		running, ok := goalRunning[g.Metric]
		if !ok || g.Target <= 0 {
			continue
		}
		// the periods of the goal the activities are in
		periods := map[time.Time]time.Time{}
		for _, a := range activities {
			if len(g.Sports) > 0 && !slices.Contains(g.Sports, a.Sport) {
				continue
			}
			start, end := g.bounds(a.Date.In(loc))
			if a.Date.Before(start) || !a.Date.Before(end) {
				continue
			}
			periods[start] = end
		}
		for start, end := range periods {
			var rows []struct {
				ID    uuid.UUID
				Title string
				Date  time.Time
				Sport string
				Total float64
			}
			query := db.Table("activities").Select("id, title, date, sport, "+running+" AS total").
				Where("user_id = ? AND date >= ? AND date < ?", userID, start, end)
			if len(g.Sports) > 0 {
				query = query.Where("sport IN ?", []string(g.Sports))
			}
			if err := query.Order("date, id").Scan(&rows).Error; err != nil {
				return nil, err
			}
			// the first activity at the target completed it
			for _, row := range rows {
				if row.Total < g.Target {
					continue
				}
				if wanted[row.ID] {
					goalID := g.ID
					highlights = append(highlights, types.Highlight{ActivityID: row.ID, ActivityTitle: row.Title,
						Date: row.Date, Kind: types.HighlightGoal, Sport: row.Sport, Target: g.Target,
						Value: row.Total, Rank: 1, GoalID: &goalID, Name: g.Name, Metric: g.Metric})
				}
				break
			}
		}
	}
	return highlights, nil
}

// highlightOrder sorts the highlights: the latest activity first, then the goals, the new bests, the rest of
// the podium; the efforts by kind and target
func highlightOrder(a, b types.Highlight) int {
	if !a.Date.Equal(b.Date) {
		return b.Date.Compare(a.Date)
	}
	weight := func(h types.Highlight) int {
		switch {
		case h.Kind == types.HighlightGoal:
			return 0
		case h.Rank == 1:
			return 1
		default:
			return 1 + h.Rank
		}
	}
	if wa, wb := weight(a), weight(b); wa != wb {
		return wa - wb
	}
	if a.Kind != b.Kind {
		if a.Kind < b.Kind {
			return -1
		}
		return 1
	}
	if a.EffortKind != b.EffortKind {
		if a.EffortKind < b.EffortKind {
			return -1
		}
		return 1
	}
	switch {
	case a.Target < b.Target:
		return -1
	case a.Target > b.Target:
		return 1
	}
	return 0
}

// GetHighlights returns the highlights of activities of a user, compared with all the activities of the user;
// the periods of their goals in loc. The activities of another user are ignored.
func (db *DB) GetHighlights(userID uint, ids []uuid.UUID, loc *time.Location) ([]types.Highlight, error) {
	var activities []highlightActivity
	if len(ids) > 0 {
		err := db.Table("activities").Select("id, title, date, sport").Where("user_id = ? AND id IN ?", userID, ids).
			Scan(&activities).Error
		if err != nil {
			return nil, err
		}
	}
	if len(activities) == 0 {
		return []types.Highlight{}, nil
	}
	own := make([]uuid.UUID, len(activities))
	for i, a := range activities {
		own[i] = a.ID
	}
	efforts, err := db.effortHighlights(userID, own)
	if err != nil {
		return nil, err
	}
	segments, err := db.segmentHighlights(userID, own)
	if err != nil {
		return nil, err
	}
	goals, err := db.goalHighlights(userID, activities, loc)
	if err != nil {
		return nil, err
	}
	highlights := append(append(goals, efforts...), segments...)
	slices.SortStableFunc(highlights, highlightOrder)
	return highlights, nil
}

// effortBests returns the best efforts of the activities that were new bests at their date: in one pass over
// the efforts of the user in the order of their dates, each one with the best before it and the best after it
// (effortHighlights compares each effort with all the others, for the podium: too slow for many activities)
func (db *DB) effortBests(userID uint, ids []uuid.UUID) ([]types.Highlight, error) {
	var rows []struct {
		types.Highlight
		LaterBest *float64
	}
	err := db.Raw(`WITH e AS (`+effortsOf+`), w AS (
			SELECT e.*,
				CASE WHEN kind = ? THEN min(value) OVER before ELSE max(value) OVER before END AS previous,
				CASE WHEN kind = ? THEN min(value) OVER after ELSE max(value) OVER after END AS later_best
			FROM e
			WINDOW before AS (PARTITION BY sport, kind, target ORDER BY date, id
					ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),
				-- in reverse order: a frame starting at the first row is computed as it grows, one ending at
				-- the last row from scratch for each row (min and max can't remove a row)
				after AS (PARTITION BY sport, kind, target ORDER BY date DESC, id DESC
					ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING))
		SELECT id AS activity_id, title AS activity_title, date, sport, kind AS effort_kind, target, value,
			start_offset, previous, later_best
		FROM w
		WHERE id IN ? AND previous IS NOT NULL
			AND CASE WHEN kind = ? THEN value < previous ELSE value > previous END`,
		userID, types.EffortDistance, types.EffortDistance, ids, types.EffortDistance).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	bests := make([]types.Highlight, len(rows))
	for i, row := range rows {
		h := row.Highlight
		h.Kind, h.Rank = types.HighlightEffort, 1
		// beaten since: a better one later; an equal one later doesn't take the record
		h.Current = row.LaterBest == nil || (h.EffortKind == types.EffortDistance && *row.LaterBest >= h.Value) ||
			(h.EffortKind != types.EffortDistance && *row.LaterBest <= h.Value)
		bests[i] = h
	}
	return bests, nil
}

// GetRecentHighlights returns the new bests (rank 1) and the goals completed of the activities of a user
// since since (the last 100 at most), at most limit, the latest first
func (db *DB) GetRecentHighlights(userID uint, since time.Time, limit int, loc *time.Location) ([]types.Highlight, error) {
	var activities []highlightActivity
	err := db.Table("activities").Select("id, title, date, sport").Where("user_id = ? AND date >= ?", userID, since).
		Order("date DESC").Limit(100).Scan(&activities).Error
	if err != nil || len(activities) == 0 {
		return []types.Highlight{}, err
	}
	ids := make([]uuid.UUID, len(activities))
	for i, a := range activities {
		ids[i] = a.ID
	}
	efforts, err := db.effortBests(userID, ids)
	if err != nil {
		return nil, err
	}
	segments, err := db.segmentHighlights(userID, ids)
	if err != nil {
		return nil, err
	}
	goals, err := db.goalHighlights(userID, activities, loc)
	if err != nil {
		return nil, err
	}
	all := append(append(goals, efforts...), segments...)
	slices.SortStableFunc(all, highlightOrder)
	recent := []types.Highlight{}
	for _, h := range all {
		if h.Rank == 1 && len(recent) < limit {
			recent = append(recent, h)
		}
	}
	return recent, nil
}

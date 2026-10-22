package models

import (
	"MySportWeb/internal/pkg/types"
	"sort"

	"github.com/google/uuid"
)

// DuplicateMinOverlap is the share of the shorter activity two activities must share to be duplicates:
// a short run during a ride isn't one
const DuplicateMinOverlap = 0.5

// FindDuplicates returns the user's groups of duplicate activities, most recent first, their activities
// in import order. Activities are duplicates when they have the same sport and start at the same time,
// or overlap for DuplicateMinOverlap of the shorter one; durations are moving times, ends are estimates.
func (db *DB) FindDuplicates(userID uint) ([]types.DuplicateGroup, error) {
	var pairs []struct{ A, B uuid.UUID }
	// the ratio is cast: next to the bigint durations, Postgres would take it for an integer
	err := db.Raw(`SELECT a.id AS a, b.id AS b FROM activities a JOIN activities b
		ON b.user_id = a.user_id AND b.sport = a.sport AND (b.date, b.id) > (a.date, a.id)
		AND b.date < a.date + interval '1 day'
		WHERE a.user_id = ? AND (b.date = a.date OR
			extract(epoch FROM least(a.date + make_interval(secs => a.duration), b.date + make_interval(secs => b.duration)) - b.date)
			>= ?::float8 * least(a.duration, b.duration) AND least(a.duration, b.duration) > 0)`,
		userID, DuplicateMinOverlap).Scan(&pairs).Error
	if err != nil {
		return nil, err
	}
	groups := []types.DuplicateGroup{}
	if len(pairs) == 0 {
		return groups, nil
	}

	// groups: the connected activities, a duplicate of a duplicate being one too
	parent := map[uuid.UUID]uuid.UUID{}
	var find func(id uuid.UUID) uuid.UUID
	find = func(id uuid.UUID) uuid.UUID {
		if p, ok := parent[id]; ok && p != id {
			parent[id] = find(p)
			return parent[id]
		}
		parent[id] = id
		return id
	}
	for _, pair := range pairs {
		parent[find(pair.A)] = find(pair.B)
	}
	ids := make([]uuid.UUID, 0, len(parent))
	for id := range parent {
		ids = append(ids, id)
	}

	var activities []types.DuplicateActivity
	err = db.Table("activities").
		Select("activities.id, activities.title, activities.date, activities.sport, activities.distance, "+
			"activities.duration, activities.avg_speed, activities.positive_elevation AS elevation, "+
			"activities.is_commute, activities.equipment_id, equipments.name AS equipment_name, "+
			"activities.created_at AS imported_at").
		Joins("LEFT JOIN equipments ON equipments.id = activities.equipment_id").
		Where("activities.id IN ?", ids).Order("activities.created_at").Order("activities.id").
		Scan(&activities).Error
	if err != nil {
		return nil, err
	}
	index := map[uuid.UUID]int{}
	for _, a := range activities {
		root := find(a.ID)
		i, ok := index[root]
		if !ok {
			i = len(groups)
			index[root] = i
			groups = append(groups, types.DuplicateGroup{Identical: true})
		}
		group := &groups[i]
		if len(group.Activities) > 0 && !group.Activities[0].Date.Equal(a.Date) {
			group.Identical = false
		}
		group.Activities = append(group.Activities, a)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Activities[0].Date.After(groups[j].Activities[0].Date)
	})
	return groups, nil
}

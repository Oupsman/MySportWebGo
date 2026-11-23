package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// the metrics a goal can target, and the periods of a goal
var (
	GoalMetrics = []string{"distance", "duration", "elevation", "activities"}
	GoalPeriods = []string{"week", "month", "year", "custom"}
)

// goalHistory is how many past periods of a recurring goal are shown
var goalHistory = map[string]int{"week": 12, "month": 12, "year": 5}

// the SQL summing a metric of the activities; the keys are the only metrics accepted
var goalSums = map[string]string{
	"distance":   "coalesce(sum(distance), 0)",
	"duration":   "coalesce(sum(duration), 0)",
	"elevation":  "coalesce(sum(positive_elevation), 0)",
	"activities": "count(*)",
}

// SportList is a list of sport names, stored as a JSON array
type SportList []string

func (s *SportList) Scan(src interface{}) error {
	*s = SportList{}
	switch v := src.(type) {
	case nil:
		return nil
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	}
	return fmt.Errorf("a sport list can't be read from %T", src)
}

func (s SportList) Value() (driver.Value, error) {
	if s == nil {
		s = SportList{}
	}
	return json.Marshal(s)
}

func (SportList) GormDataType() string {
	return "jsonb"
}

// Goal is a training goal of a user: see the goals migration. StartsOn and EndsOn are dates (midnight UTC),
// set for a custom period only.
type Goal struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	UserID      uint       `gorm:"not null" json:"-"`
	Name        string     `json:"name"`
	Metric      string     `json:"metric"`
	Target      float64    `json:"target"`
	Sports      SportList  `gorm:"not null" json:"sports"`
	Period      string     `json:"period"`
	StartsOn    *time.Time `gorm:"type:date" json:"-"`
	EndsOn      *time.Time `gorm:"type:date" json:"-"`
	NotifiedFor *time.Time `gorm:"type:date" json:"-"`
}

// GoalPeriod is a period of a goal: its first and last days (YYYY-MM-DD, included), the value of the metric
// over it
type GoalPeriod struct {
	Start   string  `json:"start"`
	End     string  `json:"end"`
	Value   float64 `json:"value"`
	Reached bool    `json:"reached"`
}

// goal states
const (
	GoalReached  = "reached"  // the target is met
	GoalAhead    = "ahead"    // at or above the steady pace
	GoalBehind   = "behind"   // below it
	GoalUpcoming = "upcoming" // a custom period not started
	GoalMissed   = "missed"   // a custom period ended, its target not met
)

// GoalStatus is a goal with its progress: the current period (the custom one, or the recurring period of
// now), the value expected by now at a steady pace, what is left per day to reach it, the past periods of a
// recurring goal (oldest first) and its streak: the periods reached in a row, ending with the current one
// when it is reached, else the last one.
type GoalStatus struct {
	Goal
	StartsOn *string      `json:"starts_on"`
	EndsOn   *string      `json:"ends_on"`
	Current  GoalPeriod   `json:"current"`
	State    string       `json:"state"`
	Expected float64      `json:"expected"`
	DaysLeft int          `json:"days_left"`
	PerDay   float64      `json:"per_day"`
	History  []GoalPeriod `json:"history"`
	Streak   int          `json:"streak"`
}

// goalPeriodAt returns the bounds [start, end) of the recurring period containing t, in the location of t
func goalPeriodAt(period string, t time.Time) (time.Time, time.Time) {
	week, month, year := dashboardPeriods(t)
	switch period {
	case "week":
		return week, week.AddDate(0, 0, 7)
	case "month":
		return month, month.AddDate(0, 1, 0)
	default:
		return year, year.AddDate(1, 0, 0)
	}
}

// inLocation is the midnight of the date d (stored as midnight UTC) in loc
func inLocation(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// bounds returns the current period of the goal at now: [start, end), in the location of now
func (g Goal) bounds(now time.Time) (time.Time, time.Time) {
	if g.Period == "custom" && g.StartsOn != nil && g.EndsOn != nil {
		return inLocation(*g.StartsOn, now.Location()), inLocation(*g.EndsOn, now.Location()).AddDate(0, 0, 1)
	}
	return goalPeriodAt(g.Period, now)
}

func newGoalPeriod(start, end time.Time, value, target float64) GoalPeriod {
	return GoalPeriod{
		Start:   start.Format(time.DateOnly),
		End:     end.AddDate(0, 0, -1).Format(time.DateOnly),
		Value:   value,
		Reached: value >= target,
	}
}

// days counts the days from a to b, both midnights of the same location (daylight saving changes kept)
func days(a, b time.Time) int {
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(db.Sub(da).Hours() / 24)
}

// progress fills the status of the current period from its value at now
func (s *GoalStatus) progress(start, end, now time.Time, value float64) {
	s.Current = newGoalPeriod(start, end, value, s.Target)
	total := float64(days(start, end))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case now.Before(start):
		s.State, s.Expected, s.DaysLeft = GoalUpcoming, 0, days(start, end)
	case !now.Before(end):
		s.Expected, s.DaysLeft = s.Target, 0
		s.State = GoalMissed
	default:
		// expected by the end of today; the days left count today: there is still time to train
		elapsed := float64(days(start, today) + 1)
		s.Expected = s.Target * elapsed / total
		s.DaysLeft = days(today, end)
		s.State = GoalBehind
		if value >= s.Expected {
			s.State = GoalAhead
		}
	}
	if s.Current.Reached {
		s.State = GoalReached
	}
	if s.DaysLeft > 0 && value < s.Target {
		s.PerDay = (s.Target - value) / float64(s.DaysLeft)
	}
}

// goalSum returns the sum of the metric of the activities of the user of the goal in [from, to)
func (db *DB) goalSum(g Goal, from, to time.Time) (float64, error) {
	var value float64
	query := db.Table("activities").Select(goalSums[g.Metric]).
		Where("user_id = ? AND date >= ? AND date < ?", g.UserID, from, to)
	if len(g.Sports) > 0 {
		query = query.Where("sport IN ?", []string(g.Sports))
	}
	err := query.Scan(&value).Error
	return value, err
}

// goalHistorySums returns the sums of the metric per recurring period of the goal from first to before
// (period starts), by the first day of the period (YYYY-MM-DD)
func (db *DB) goalHistorySums(g Goal, first, before time.Time) (map[string]float64, error) {
	var rows []struct {
		PeriodStart string
		Value       float64
	}
	// g.Period is one of GoalPeriods, checked before saving: date_trunc starts the weeks on Monday
	query := db.Table("activities").
		Select("to_char(date_trunc(?, date AT TIME ZONE ?), 'YYYY-MM-DD') AS period_start, "+goalSums[g.Metric]+" AS value",
			g.Period, first.Location().String()).
		Where("user_id = ? AND date >= ? AND date < ?", g.UserID, first, before)
	if len(g.Sports) > 0 {
		query = query.Where("sport IN ?", []string(g.Sports))
	}
	if err := query.Group("period_start").Scan(&rows).Error; err != nil {
		return nil, err
	}
	sums := make(map[string]float64, len(rows))
	for _, r := range rows {
		sums[r.PeriodStart] = r.Value
	}
	return sums, nil
}

// GoalStatusAt computes the progress of a goal at now, in the timezone of now
func (db *DB) GoalStatusAt(g Goal, now time.Time) (GoalStatus, error) {
	return db.goalStatusAt(g, now, true)
}

// goalStatusAt computes the progress of a goal at now, its past periods when history
func (db *DB) goalStatusAt(g Goal, now time.Time, history bool) (GoalStatus, error) {
	if _, ok := goalSums[g.Metric]; !ok {
		return GoalStatus{}, fmt.Errorf("unknown goal metric %q", g.Metric)
	}
	s := GoalStatus{Goal: g, History: []GoalPeriod{}}
	if g.StartsOn != nil && g.EndsOn != nil {
		starts, ends := g.StartsOn.Format(time.DateOnly), g.EndsOn.Format(time.DateOnly)
		s.StartsOn, s.EndsOn = &starts, &ends
	}
	start, end := g.bounds(now)
	value, err := db.goalSum(g, start, end)
	if err != nil {
		return s, err
	}
	s.progress(start, end, now, value)

	n, recurring := goalHistory[g.Period]
	if !recurring || !history {
		return s, nil
	}
	// the past periods, the latest first: each one contains the day before the next one
	starts := make([]time.Time, n)
	next := start
	for i := range starts {
		starts[i], _ = goalPeriodAt(g.Period, next.AddDate(0, 0, -1))
		next = starts[i]
	}
	sums, err := db.goalHistorySums(g, starts[n-1], start)
	if err != nil {
		return s, err
	}
	for i := n - 1; i >= 0; i-- {
		_, periodEnd := goalPeriodAt(g.Period, starts[i])
		s.History = append(s.History, newGoalPeriod(starts[i], periodEnd, sums[starts[i].Format(time.DateOnly)], g.Target))
	}
	if s.Current.Reached {
		s.Streak = 1
	}
	for i := len(s.History) - 1; i >= 0 && s.History[i].Reached; i-- {
		s.Streak++
	}
	return s, nil
}

// GetGoals returns the goals of the user with their progress at now, oldest first
func (db *DB) GetGoals(userID uint, now time.Time) ([]GoalStatus, error) {
	var goals []Goal
	if err := db.Where("user_id = ?", userID).Order("created_at, id").Find(&goals).Error; err != nil {
		return nil, err
	}
	statuses := make([]GoalStatus, 0, len(goals))
	for _, g := range goals {
		s, err := db.GoalStatusAt(g, now)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

// GetGoal returns a goal of the user, ErrRecordNotFound when they don't have it
func (db *DB) GetGoal(userID uint, id uuid.UUID) (Goal, error) {
	var goal Goal
	err := db.Where("id = ? AND user_id = ?", id, userID).First(&goal).Error
	return goal, err
}

// SaveGoal creates or updates a goal. Already reached in its current period at now, that period counts as
// notified: a goal isn't announced as reached the moment it is set.
func (db *DB) SaveGoal(g *Goal, now time.Time) error {
	s, err := db.GoalStatusAt(*g, now)
	if err != nil {
		return err
	}
	g.NotifiedFor = nil
	if s.Current.Reached {
		start, _ := g.bounds(now)
		day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		g.NotifiedFor = &day
	}
	if g.ID == uuid.Nil {
		return db.Create(g).Error
	}
	return db.Model(g).Select("name", "metric", "target", "sports", "period", "starts_on", "ends_on",
		"notified_for", "updated_at").Updates(g).Error
}

// DeleteGoal deletes a goal of the user, ErrRecordNotFound when they don't have it
func (db *DB) DeleteGoal(userID uint, id uuid.UUID) error {
	result := db.Where("id = ? AND user_id = ?", id, userID).Delete(&Goal{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GoalsToNotify returns the goals reached in their current period at now (in the timezone of their user)
// and not notified for it yet, of the users with a push subscription or the notifications by email: the
// others are notified once they have one. A custom period is notified until its end only.
func (db *DB) GoalsToNotify(now time.Time) ([]GoalStatus, error) {
	var rows []struct {
		Goal     `gorm:"embedded"`
		Timezone string
	}
	err := db.Table("goals").Select("goals.*, users.timezone").Joins("JOIN users ON users.id = goals.user_id").
		Where("users.disabled_at IS NULL AND goals.user_id IN (SELECT user_id FROM channels "+
			"WHERE channel_provider IN ? AND deleted_at IS NULL)", notifyingProviders).
		Order("goals.user_id, goals.created_at").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	var reached []GoalStatus
	for _, row := range rows {
		at := now.In(locationOf(row.Timezone))
		start, end := row.Goal.bounds(at)
		if at.Before(start) || !at.Before(end) {
			continue
		}
		if row.NotifiedFor != nil && row.NotifiedFor.Format(time.DateOnly) == start.Format(time.DateOnly) {
			continue
		}
		s, err := db.goalStatusAt(row.Goal, at, false)
		if err != nil {
			return nil, err
		}
		if s.Current.Reached {
			reached = append(reached, s)
		}
	}
	return reached, nil
}

// SetGoalNotified records that the period of a goal starting on start (YYYY-MM-DD) was notified
func (db *DB) SetGoalNotified(id uuid.UUID, start string) error {
	return db.Model(&Goal{}).Where("id = ?", id).UpdateColumn("notified_for", start).Error
}

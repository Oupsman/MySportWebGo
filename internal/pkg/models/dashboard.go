package models

import (
	"MySportWeb/internal/pkg/types"
	"time"
)

// DashboardWeeks is the number of weeks of the weekly volume chart
const DashboardWeeks = 12

// dashboardPeriods returns the start of the current week (Monday), month and year in loc
func dashboardPeriods(now time.Time) (week, month, year time.Time) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// Monday is the first day of the week
	week = day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	month = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	year = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
	return week, month, year
}

const totalsColumns = "count(*) AS activities, coalesce(sum(distance), 0) AS distance, " +
	"coalesce(sum(duration), 0) AS duration, coalesce(sum(positive_elevation), 0) AS elevation"

// periodTotals sums the user's activities between from (included) and to (excluded), zero times meaning no bound
func (db *DB) periodTotals(userID uint, from, to time.Time) (types.PeriodTotals, error) {
	var totals types.PeriodTotals
	query := db.Table("activities").Select(totalsColumns).Where("user_id = ?", userID)
	if !from.IsZero() {
		query = query.Where("date >= ?", from)
	}
	if !to.IsZero() {
		query = query.Where("date < ?", to)
	}
	err := query.Scan(&totals).Error
	return totals, err
}

// sportRecords returns, for each sport, the activity with the highest value of column
func (db *DB) sportRecords(userID uint, column string) (map[string]*types.SportRecord, error) {
	var rows []struct {
		Sport string
		types.SportRecord
	}
	// column comes from the callers' constants, never from a request
	err := db.Raw("SELECT DISTINCT ON (sport) sport, id, title, date, "+column+" AS value FROM activities "+
		"WHERE user_id = ? AND "+column+" > 0 ORDER BY sport, "+column+" DESC, date", userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	records := map[string]*types.SportRecord{}
	for i := range rows {
		records[rows[i].Sport] = &rows[i].SportRecord
	}
	return records, nil
}

// GetCalendar returns the user's activities of the month starting at month (the first day at midnight,
// in the user's timezone), oldest first
func (db *DB) GetCalendar(userID uint, month time.Time) ([]types.ActivityCalendar, error) {
	calendar := []types.ActivityCalendar{}
	err := db.Table("activities").Select("id, title, date, sport, distance, duration").
		Where("user_id = ? AND date >= ? AND date < ?", userID, month, month.AddDate(0, 1, 0)).
		Order("date").Scan(&calendar).Error
	return calendar, err
}

// GetDashboard computes the dashboard of a user, periods are in the timezone of now
func (db *DB) GetDashboard(userID uint, now time.Time) (types.Dashboard, error) {
	var dashboard types.Dashboard
	var err error
	dashboard.Timezone = now.Location().String()
	week, month, year := dashboardPeriods(now)

	periods := []struct {
		totals   *types.PeriodTotals
		from, to time.Time
	}{
		{&dashboard.Totals.ThisWeek, week, time.Time{}},
		{&dashboard.Totals.LastWeek, week.AddDate(0, 0, -7), week},
		{&dashboard.Totals.ThisMonth, month, time.Time{}},
		{&dashboard.Totals.LastMonth, month.AddDate(0, -1, 0), month},
		{&dashboard.Totals.ThisYear, year, time.Time{}},
		{&dashboard.Totals.AllTime, time.Time{}, time.Time{}},
	}
	for _, period := range periods {
		if *period.totals, err = db.periodTotals(userID, period.from, period.to); err != nil {
			return dashboard, err
		}
	}

	// weekly volume per sport, weeks computed in the user's timezone
	firstWeek := week.AddDate(0, 0, -7*(DashboardWeeks-1))
	for i := 0; i < DashboardWeeks; i++ {
		dashboard.WeekStarts = append(dashboard.WeekStarts, firstWeek.AddDate(0, 0, 7*i).Format(time.DateOnly))
	}
	dashboard.Weekly = []types.WeeklyVolume{}
	err = db.Raw("SELECT to_char(date_trunc('week', date AT TIME ZONE ?), 'YYYY-MM-DD') AS week, sport, "+
		"count(*) AS activities, coalesce(sum(distance), 0) AS distance, coalesce(sum(duration), 0) AS duration "+
		"FROM activities WHERE user_id = ? AND date >= ? GROUP BY 1, 2 ORDER BY 1, 2",
		dashboard.Timezone, userID, firstWeek).Scan(&dashboard.Weekly).Error
	if err != nil {
		return dashboard, err
	}

	// totals and records per sport, most practiced first
	var sports []struct {
		Sport string
		types.PeriodTotals
	}
	err = db.Table("activities").Select("sport, "+totalsColumns).Where("user_id = ?", userID).
		Group("sport").Order("activities DESC").Scan(&sports).Error
	if err != nil {
		return dashboard, err
	}
	distances, err := db.sportRecords(userID, "distance")
	if err != nil {
		return dashboard, err
	}
	durations, err := db.sportRecords(userID, "duration")
	if err != nil {
		return dashboard, err
	}
	climbs, err := db.sportRecords(userID, "positive_elevation")
	if err != nil {
		return dashboard, err
	}
	dashboard.Sports = []types.SportSummary{}
	for _, sport := range sports {
		dashboard.Sports = append(dashboard.Sports, types.SportSummary{
			Sport:           sport.Sport,
			Totals:          sport.PeriodTotals,
			LongestDistance: distances[sport.Sport],
			LongestDuration: durations[sport.Sport],
			BiggestClimb:    climbs[sport.Sport],
		})
	}

	dashboard.Recent = []types.RecentActivity{}
	err = db.Table("activities").
		Select("id, title, date, sport, distance, duration, avg_speed, positive_elevation AS elevation").
		Where("user_id = ?", userID).Order("date DESC").Limit(10).Scan(&dashboard.Recent).Error
	if err != nil {
		return dashboard, err
	}

	if dashboard.Calendar, err = db.GetCalendar(userID, month); err != nil {
		return dashboard, err
	}

	err = db.Table("equipments").Where("user_id = ?", userID).Count(&dashboard.NbEquipments).Error
	return dashboard, err
}

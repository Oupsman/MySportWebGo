package models

import (
	"testing"
	"time"
)

func TestGoalPeriods(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	// a Wednesday
	now := time.Date(2026, 3, 25, 15, 0, 0, 0, paris)
	for _, c := range []struct {
		period, start, end string
	}{
		{"week", "2026-03-23", "2026-03-30"},
		{"month", "2026-03-01", "2026-04-01"},
		{"year", "2026-01-01", "2027-01-01"},
	} {
		start, end := goalPeriodAt(c.period, now)
		if start.Format(time.DateOnly) != c.start || end.Format(time.DateOnly) != c.end || start.Location() != paris {
			t.Errorf("%s: %v to %v", c.period, start, end)
		}
	}
	// the week of the change to summer time (March 29) still counts 7 days
	if d := days(time.Date(2026, 3, 23, 0, 0, 0, 0, paris), time.Date(2026, 3, 30, 0, 0, 0, 0, paris)); d != 7 {
		t.Errorf("days: %d", d)
	}
	starts := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	ends := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	start, end := Goal{Period: "custom", StartsOn: &starts, EndsOn: &ends}.bounds(now)
	if start != time.Date(2026, 4, 1, 0, 0, 0, 0, paris) || end != time.Date(2026, 5, 1, 0, 0, 0, 0, paris) {
		t.Errorf("custom: %v to %v", start, end)
	}
}

func TestGoalProgress(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 30)
	at := func(day, hour int) time.Time { return time.Date(2026, 4, day, hour, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		name     string
		now      time.Time
		value    float64
		state    string
		expected float64
		daysLeft int
		perDay   float64
	}{
		// April 10: 10 days of 30 elapsed by the end of today, 21 days left counting today
		{"behind", at(10, 12), 200, GoalBehind, 1000, 21, 2800.0 / 21},
		{"ahead", at(10, 12), 1000, GoalAhead, 1000, 21, 2000.0 / 21},
		{"reached", at(10, 12), 3000, GoalReached, 1000, 21, 0},
		{"upcoming", time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC), 0, GoalUpcoming, 0, 30, 100},
		{"missed", at(30, 0).AddDate(0, 0, 1), 2000, GoalMissed, 3000, 0, 0},
		{"reached after the end", at(30, 0).AddDate(0, 0, 1), 3000, GoalReached, 3000, 0, 0},
		// the last day: all of it expected by tonight, today left
		{"last day", at(30, 8), 2900, GoalBehind, 3000, 1, 100},
	} {
		s := GoalStatus{Goal: Goal{Target: 3000}}
		s.progress(start, end, c.now, c.value)
		if s.State != c.state || s.Expected != c.expected || s.DaysLeft != c.daysLeft || s.PerDay != c.perDay {
			t.Errorf("%s: %s expected %v, %d days left, %v per day", c.name, s.State, s.Expected, s.DaysLeft, s.PerDay)
		}
		if s.Current.Start != "2026-04-01" || s.Current.End != "2026-04-30" {
			t.Errorf("%s: period %s to %s", c.name, s.Current.Start, s.Current.End)
		}
	}
}

package models

import (
	"testing"
	"time"
)

func TestAddInterval(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, paris) }
	cases := []struct {
		from  time.Time
		count int
		unit  string
		want  string
	}{
		{at(2026, time.January, 31, 22), 1, UnitMonth, "2026-02-28"},
		{at(2028, time.January, 31, 10), 1, UnitMonth, "2028-02-29"},
		{at(2026, time.March, 31, 10), 3, UnitMonth, "2026-06-30"},
		{at(2026, time.November, 15, 10), 2, UnitMonth, "2027-01-15"},
		{at(2028, time.February, 29, 10), 1, UnitYear, "2029-02-28"},
		{at(2026, time.March, 28, 10), 1, UnitWeek, "2026-04-04"}, // over the change to summer time
		{at(2026, time.December, 30, 10), 3, UnitDay, "2027-01-02"},
		// 23:30 in Paris is the day before in UTC: the day is the user's
		{time.Date(2026, time.May, 1, 22, 30, 0, 0, time.UTC), 1, UnitDay, "2026-05-03"},
	}
	for _, c := range cases {
		got := AddInterval(c.from, c.count, c.unit, paris)
		if got.Format(time.DateOnly) != c.want || got.Hour() != 0 || got.Location() != paris {
			t.Errorf("%v + %d %s = %v, want %s at midnight in Paris", c.from, c.count, c.unit, got, c.want)
		}
	}
}

func TestReminderEvaluate(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	at := func(m time.Month, d, h, min int) time.Time { return time.Date(2026, m, d, h, min, 0, 0, paris) }
	lastDone := at(time.March, 1, 18, 0)
	status := func(r MaintenanceReminder, km float64, hours float64, uses int64, now time.Time) ReminderStatus {
		s := ReminderStatus{MaintenanceReminder: r, Timezone: "Europe/Paris", LastDone: &lastDone,
			DistanceSince: km * 1000, DurationSince: int64(hours * 3600), UsesSince: uses}
		s.evaluate(now)
		return s
	}
	now := at(time.March, 10, 12, 0)

	km := MaintenanceReminder{EveryKm: 3000}
	if s := status(km, 1500, 0, 3, now); s.State != ReminderOK || *s.KmLeft != 1500 || s.Progress != 0.5 || s.Notifiable {
		t.Errorf("half way: %+v", s)
	}
	if s := status(km, 2750, 0, 3, now); s.State != ReminderSoon || *s.KmLeft != 250 || s.Notifiable {
		t.Errorf("last tenth: %+v", s)
	}
	if s := status(km, 3100, 0, 3, now); s.State != ReminderDue || *s.KmLeft != -100 || !s.Notifiable || s.Progress < 1 {
		t.Errorf("overdue: %+v", s)
	}

	hours := MaintenanceReminder{EveryHours: 50}
	if s := status(hours, 0, 50.04, 20, now); s.State != ReminderDue || *s.HoursLeft != 0 || s.HoursSince != 50 {
		t.Errorf("hours: %+v", s)
	}

	// done on the 1st: due on the 8th, soon from the 7th, notified from 08:00 of the 8th
	weekly := MaintenanceReminder{EveryCount: 1, EveryUnit: UnitWeek, OnlyWhenUsed: true}
	for _, c := range []struct {
		now    time.Time
		state  string
		notify bool
	}{
		{at(time.March, 6, 23, 0), ReminderOK, false},
		{at(time.March, 7, 0, 0), ReminderSoon, false},
		{at(time.March, 8, 7, 59), ReminderDue, false},
		{at(time.March, 8, 8, 0), ReminderDue, true},
	} {
		s := status(weekly, 10, 1, 1, c.now)
		if s.State != c.state || s.Notifiable != c.notify || *s.DueDate != "2026-03-08" {
			t.Errorf("weekly at %v: %s notifiable %v due %s, want %s %v", c.now, s.State, s.Notifiable, *s.DueDate, c.state, c.notify)
		}
	}
	// not used since: waits, unless it doesn't have to
	if s := status(weekly, 0, 0, 0, at(time.March, 20, 12, 0)); s.State != ReminderOK || !s.WaitingUse || s.Notifiable || s.Progress != 1 {
		t.Errorf("waiting for a use: %+v", s)
	}
	always := weekly
	always.OnlyWhenUsed = false
	if s := status(always, 0, 0, 0, at(time.March, 20, 12, 0)); s.State != ReminderDue || !s.Notifiable || s.WaitingUse {
		t.Errorf("not only when used: %+v", s)
	}

	// the first interval reached makes it due: km before the month
	both := MaintenanceReminder{EveryKm: 500, EveryCount: 1, EveryUnit: UnitMonth, OnlyWhenUsed: true}
	if s := status(both, 520, 0, 4, now); s.State != ReminderDue || !s.Notifiable || *s.DueDate != "2026-04-01" {
		t.Errorf("km first: %+v", s)
	}

	// notified for this cycle: not again, until it is done again
	notified := km
	notified.Notified, notified.NotifiedFor = true, &lastDone
	if s := status(notified, 3100, 0, 3, now); s.Notifiable {
		t.Error("notified twice in a cycle")
	}
	earlier := at(time.February, 1, 10, 0)
	notified.NotifiedFor = &earlier
	if s := status(notified, 3100, 0, 3, now); !s.Notifiable {
		t.Error("a new cycle not notified")
	}
}

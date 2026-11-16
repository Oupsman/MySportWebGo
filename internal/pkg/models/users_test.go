package models

import (
	"testing"
	"time"
)

func TestGetAge(t *testing.T) {
	utc := func(value string) time.Time {
		date, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return date
	}
	for _, tt := range []struct {
		name     string
		birth    string // as stored
		timezone string
		on       string
		want     int
	}{
		// was 46: the birthday counted as passed because its month is later but its day earlier
		{"birthday later in the year, earlier day", "1980-08-10T00:00:00Z", "UTC", "2026-03-20T10:00:00Z", 45},
		{"birthday earlier in the year, later day", "1980-03-25T00:00:00Z", "UTC", "2026-08-10T10:00:00Z", 46},
		{"the day before the birthday", "1980-05-17T00:00:00Z", "UTC", "2026-05-16T23:59:59Z", 45},
		{"the birthday", "1980-05-17T00:00:00Z", "UTC", "2026-05-17T00:00:00Z", 46},
		// the frontend saves 17 May 1980 in Paris as a midnight there: 16 May 22:00 UTC
		{"Paris birth date, the birthday morning", "1980-05-16T22:00:00Z", "Europe/Paris", "2026-05-17T06:00:00Z", 46},
		{"Paris birth date, the evening before", "1980-05-16T22:00:00Z", "Europe/Paris", "2026-05-16T20:00:00Z", 45},
		// 23:30 UTC on 16 May is already the 17th in Paris
		{"activity after midnight in the user's timezone", "1980-05-16T22:00:00Z", "Europe/Paris", "2026-05-16T22:30:00Z", 46},
		{"Montreal birth date", "1980-05-17T04:00:00Z", "America/Montreal", "2026-05-17T03:00:00Z", 45},
		{"Montreal birthday", "1980-05-17T04:00:00Z", "America/Montreal", "2026-05-17T05:00:00Z", 46},
		{"29 February, the 28th of a common year", "2000-02-29T00:00:00Z", "UTC", "2025-02-28T12:00:00Z", 24},
		{"29 February, 1 March of a common year", "2000-02-29T00:00:00Z", "UTC", "2025-03-01T12:00:00Z", 25},
		{"29 February, a leap year", "2000-02-29T00:00:00Z", "UTC", "2024-02-29T12:00:00Z", 24},
		{"unknown timezone: UTC", "1980-05-17T00:00:00Z", "Mars/Olympus", "2026-05-17T00:00:00Z", 46},
	} {
		user := Users{DateOfBirth: utc(tt.birth), Timezone: tt.timezone}
		if got := user.GetAge(utc(tt.on)); got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}

	if age := (&Users{Timezone: "UTC"}).GetAge(time.Now()); age != -1 {
		t.Errorf("without date of birth: %d, want -1", age)
	}
}

func TestGetMaxHR(t *testing.T) {
	on := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	born := time.Date(1980, 5, 17, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		user Users
		want uint16
	}{
		{"man of 46", Users{DateOfBirth: born, Timezone: "UTC"}, 174},
		{"woman of 46", Users{DateOfBirth: born, Gender: 1, Timezone: "UTC"}, 180},
		{"measured maximum", Users{DateOfBirth: born, MaxHR: 191, Timezone: "UTC"}, 191},
		{"measured maximum, no date of birth", Users{MaxHR: 191}, 191},
		// was 220 - 2025 wrapped around: 63731
		{"no date of birth", Users{Timezone: "UTC"}, 0},
		{"born in the future", Users{DateOfBirth: on.AddDate(1, 0, 0), Timezone: "UTC"}, 0},
		{"born 200 years ago", Users{DateOfBirth: on.AddDate(-200, 0, 0), Timezone: "UTC"}, 0},
	} {
		if got := tt.user.GetMaxHR(on); got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/cities"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// periods of the day, by their first hour; before the first one it's still the night
var dayPeriods = []struct {
	from int
	name string
}{{1, "morning"}, {12, "lunch"}, {14, "afternoon"}, {18, "evening"}, {22, "night"}}

// sportNouns names an activity of a sport, the other sports are named after themselves
var sportNouns = map[string]string{
	"cycling":  "ride",
	"running":  "run",
	"walking":  "walk",
	"hiking":   "hike",
	"swimming": "swim",
}

// userLocation returns the user's timezone, false when it's unknown
func userLocation(timezone string) (*time.Location, bool) {
	location, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" {
		return time.UTC, false
	}
	return location, true
}

// UserLocation returns the user's timezone, UTC when it's unknown
func UserLocation(timezone string) *time.Location {
	location, _ := userLocation(timezone)
	return location
}

func dayPeriod(hour int) string {
	period := "night"
	for _, p := range dayPeriods {
		if hour >= p.from {
			period = p.name
		}
	}
	return period
}

// Title names an activity after the city it starts in and the time of the day in loc:
// "Paris morning ride", or "Morning ride" without start position or city nearby
func Title(sport string, date time.Time, loc *time.Location, start *types.GpsPoint) string {
	noun, ok := sportNouns[sport]
	if !ok {
		noun = strings.ReplaceAll(sport, "_", " ")
	}
	title := dayPeriod(date.In(loc).Hour()) + " " + noun
	if start != nil {
		if city := cities.Nearest(*start); city != "" {
			return city + " " + title
		}
	}
	return strings.ToUpper(title[:1]) + title[1:]
}

// legacyTitles are the titles given before Title existed, "Morning cycling" (hours in UTC)
func legacyTitles(sport string) []string {
	titles := []string{}
	for _, p := range dayPeriods {
		titles = append(titles, strings.ToUpper(p.name[:1])+p.name[1:]+" "+sport)
	}
	return titles
}

// BackfillTitles renames the activities still having a legacy title, the ones renamed by their user are kept.
// The activities of users without timezone wait for it: once renamed, they couldn't be fixed anymore.
func BackfillTitles(db *models.DB, logger zerolog.Logger) error {
	activities, err := db.GetActivitiesToRetitle(legacyTitles)
	if err != nil {
		return err
	}
	if len(activities) == 0 {
		return nil
	}
	renamed, skipped := 0, 0
	for _, a := range activities {
		location, ok := userLocation(a.Timezone)
		if !ok {
			skipped++
			continue
		}
		title := Title(a.Sport, a.Date, location, a.StartPosition)
		if err := db.SetActivityTitle(a.ID, a.Title, title); err != nil {
			return err
		}
		renamed++
	}
	if skipped > 0 {
		logger.Warn().Int("activities", skipped).Msg("activities not renamed: their user has no timezone")
	}
	if renamed > 0 {
		logger.Info().Int("activities", renamed).Msg("activities renamed")
	}
	return nil
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/weather"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Weather is the source of the weather of the activities, nil without (WEATHER_URL=none): set at the start
var Weather *weather.Client

// The pace of the backfill of the weather: Open-Meteo's free service accepts 5000 requests an hour and
// 10000 a day, shared with the uploads
var (
	WeatherPause           = 1500 * time.Millisecond // between two requests: 2400 an hour at most
	MaxWeatherRequestsRun  = 6000                    // per start: the others at the next one
	maxWeatherFailuresKept = 5                       // errors in a row (the source unreachable): stopped
)

// weatherKey groups the activities started the same day (UTC) at the same place, rounded as sent: one request
type weatherKey struct {
	latitude, longitude float64
	day                 string
}

func weatherKeyOf(start types.GpsPoint, date time.Time) weatherKey {
	return weatherKey{weather.Round(start.Lat), weather.Round(start.Lon),
		date.UTC().Round(time.Hour).Format(time.DateOnly)}
}

// saveWeather saves the weather of an activity from the day of its place: unavailable when the source has
// none for its hour
func saveWeather(db *models.DB, id uuid.UUID, date time.Time, start types.GpsPoint, day weather.Day) error {
	w, err := day.At(date)
	if errors.Is(err, weather.ErrUnavailable) {
		w = weather.Weather{Time: date.UTC().Round(time.Hour), Latitude: weather.Round(start.Lat),
			Longitude: weather.Round(start.Lon), Source: "open-meteo", Unavailable: true}
	} else if err != nil {
		return err
	}
	return db.SetActivityWeather(id, w)
}

// FetchActivityWeather gets and saves the weather at the start of an activity: after its upload. Nothing
// without weather source or start position.
func FetchActivityWeather(ctx context.Context, db *models.DB, id uuid.UUID, date time.Time, start *types.GpsPoint) error {
	if Weather == nil || start == nil || !ValidPosition(start) {
		return nil
	}
	day, err := Weather.Day(ctx, start.Lat, start.Lon, date.UTC().Round(time.Hour))
	if err != nil && !errors.Is(err, weather.ErrUnavailable) {
		return err
	}
	return saveWeather(db, id, date, *start, day)
}

// BackfillWeather gets the weather of the activities without it, the most recent first: the ones of the same
// day and place in one request, a request every WeatherPause, at most MaxWeatherRequestsRun per start. It
// stops when the source refuses (too many requests) or keeps failing: the others at the next start.
func BackfillWeather(ctx context.Context, db *models.DB, logger zerolog.Logger) error {
	if Weather == nil {
		return nil
	}
	activities, err := db.GetActivitiesWithoutWeather(MaxWeatherRequestsRun * 10)
	if err != nil || len(activities) == 0 {
		return err
	}
	// in the order of the activities, the most recent first
	var order []weatherKey
	groups := map[weatherKey][]models.WeatherToFetch{}
	for _, a := range activities {
		// no place to ask about: not selected again
		if !ValidPosition(&a.StartPosition) {
			if err := db.SetActivityWeather(a.ID, weather.Weather{Unavailable: true}); err != nil {
				return err
			}
			continue
		}
		key := weatherKeyOf(a.StartPosition, a.Date)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], a)
	}
	if len(order) > MaxWeatherRequestsRun {
		order = order[:MaxWeatherRequestsRun]
	}
	logger = logger.With().Str("backfill", "weather").Logger()
	progress := newBackfillProgress(ctx, logger, "activities", len(activities), "getting the weather of activities")
	done, failures, saved := 0, 0, 0
	for i, key := range order {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(WeatherPause):
			}
		}
		first := groups[key][0]
		day, err := Weather.Day(ctx, first.StartPosition.Lat, first.StartPosition.Lon, first.Date.UTC().Round(time.Hour))
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, weather.ErrRateLimited):
			logger.Warn().Int("left", len(activities)-done).
				Msg("weather source: too many requests, the others at the next start")
			return nil
		case err != nil && !errors.Is(err, weather.ErrUnavailable):
			failures++
			logger.Warn().Err(err).Msg("weather not read")
			if failures >= maxWeatherFailuresKept {
				logger.Warn().Int("left", len(activities)-done).
					Msg("weather source failing: the others at the next start")
				return nil
			}
			done += len(groups[key])
			continue
		}
		failures = 0
		for _, a := range groups[key] {
			if err := saveWeather(db, a.ID, a.Date, a.StartPosition, day); err != nil {
				return err
			}
			saved++
		}
		done += len(groups[key])
		progress.done(min(done, len(activities)), nil)
	}
	logger.Info().Int("activities", saved).Int("requests", len(order)).Str("elapsed", progress.elapsed()).
		Msg("weather of activities got")
	return nil
}

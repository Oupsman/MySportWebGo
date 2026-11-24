// Package weather reads the weather of the past hours from Open-Meteo (https://open-meteo.com, CC BY 4.0: the
// pages showing it link to it). The coordinates sent are rounded to RoundDegrees: the start of an activity is
// often its user's home, and the weather of the past is as precise at that scale (reanalyses of 10 to 25 km).
package weather

import (
	"MySportWeb/internal/pkg/metrics"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RoundDegrees is the step the coordinates sent are rounded to: 0.1° is 11 km of latitude
const RoundDegrees = 0.1

// The public servers of Open-Meteo, free for a non-commercial use (less than 10000 calls a day)
const (
	ArchiveURL  = "https://archive-api.open-meteo.com/v1/archive"
	ForecastURL = "https://api.open-meteo.com/v1/forecast"
	// ForecastPastDays: the forecast API answers the hours of the last that many days, the archive the older
	// ones (a few days late for some of its models)
	ForecastPastDays = 92
)

// UserAgent identifies the application to Open-Meteo (set at the start, with the version)
var UserAgent = "MySportWeb (self-hosted sport activity tracker)"

// ErrRateLimited: Open-Meteo refused the request, too many of them; ErrUnavailable: it has no value for that hour
var (
	ErrRateLimited = errors.New("open-meteo: too many requests")
	ErrUnavailable = errors.New("open-meteo: no weather for that hour")
)

// variables are the hourly values asked: 10 at most, a request of more counts as several calls
var variables = []string{"temperature_2m", "apparent_temperature", "relative_humidity_2m", "precipitation",
	"weather_code", "cloud_cover", "wind_speed_10m", "wind_gusts_10m", "wind_direction_10m"}

// Weather is the weather of an hour at a place; a value is nil when the source doesn't have it
type Weather struct {
	Time                time.Time `json:"time"`                 // the hour of the values, UTC
	Latitude            float64   `json:"latitude"`             // the place asked, rounded
	Longitude           float64   `json:"longitude"`            //
	Temperature         *float64  `json:"temperature"`          // °C at 2 m
	ApparentTemperature *float64  `json:"apparent_temperature"` // °C, with the wind and the humidity
	Humidity            *float64  `json:"humidity"`             // % at 2 m
	Precipitation       *float64  `json:"precipitation"`        // mm over the hour ending at Time
	Code                *int      `json:"code"`                 // WMO weather code
	CloudCover          *float64  `json:"cloud_cover"`          // %
	WindSpeed           *float64  `json:"wind_speed"`           // km/h at 10 m
	WindGusts           *float64  `json:"wind_gusts"`           // km/h at 10 m
	WindDirection       *float64  `json:"wind_direction"`       // degrees it comes from, 0 the north
	Source              string    `json:"source"`
	// Unavailable: the source has no weather for that hour and place, it isn't asked again
	Unavailable bool `json:"unavailable,omitempty"`
}

// Day is the hourly weather of a day at a place, by hour (UTC)
type Day map[time.Time]Weather

// Client reads the weather from Open-Meteo, or from a server of the same API (a self-hosted Open-Meteo)
type Client struct {
	archive, forecast string
	http              *http.Client
	now               func() time.Time
}

// New returns a client of the public servers of Open-Meteo for base "", of a server of the same API at base
// otherwise (its /v1/archive and /v1/forecast); nil for "none": no weather
func New(base string) *Client {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	client := &Client{archive: ArchiveURL, forecast: ForecastURL, http: &http.Client{Timeout: 30 * time.Second},
		now: time.Now}
	switch base {
	case "none":
		return nil
	case "":
	default:
		client.archive, client.forecast = base+"/v1/archive", base+"/v1/forecast"
	}
	return client
}

// Round rounds a coordinate to RoundDegrees (a tenth), the precision sent: divided by 10, the nearest
// float64 to the tenth, no 48.900000000000006; never -0
func Round(degrees float64) float64 {
	rounded := math.Round(degrees*10) / 10
	if rounded == 0 {
		return 0
	}
	return rounded
}

// response is the answer of both APIs, the times in Unix seconds (timeformat=unixtime)
type response struct {
	Hourly struct {
		Time                []int64    `json:"time"`
		Temperature         []*float64 `json:"temperature_2m"`
		ApparentTemperature []*float64 `json:"apparent_temperature"`
		Humidity            []*float64 `json:"relative_humidity_2m"`
		Precipitation       []*float64 `json:"precipitation"`
		Code                []*float64 `json:"weather_code"`
		CloudCover          []*float64 `json:"cloud_cover"`
		WindSpeed           []*float64 `json:"wind_speed_10m"`
		WindGusts           []*float64 `json:"wind_gusts_10m"`
		WindDirection       []*float64 `json:"wind_direction_10m"`
	} `json:"hourly"`
	Reason string `json:"reason"` // of an error
}

// Day returns the hourly weather of the day (UTC) of day at the place, its coordinates rounded first: from
// the archive, from the forecast API for the hours of the last ForecastPastDays the archive lacks
func (c *Client) Day(ctx context.Context, latitude, longitude float64, day time.Time) (Day, error) {
	latitude, longitude = Round(latitude), Round(longitude)
	date := day.UTC().Format(time.DateOnly)
	hours, err := c.get(ctx, c.archive, latitude, longitude, date)
	if err != nil && !errors.Is(err, ErrUnavailable) {
		return nil, err
	}
	if complete(hours) || c.now().Sub(day) > ForecastPastDays*24*time.Hour {
		return hours, nil
	}
	recent, err := c.get(ctx, c.forecast, latitude, longitude, date)
	if err != nil {
		return hours, err
	}
	// the archive's hours first: the forecast API fills the others
	for hour, w := range recent {
		if old, ok := hours[hour]; !ok || old.Temperature == nil {
			if hours == nil {
				hours = Day{}
			}
			hours[hour] = w
		}
	}
	return hours, nil
}

// complete tells whether every hour of a day has its temperature
func complete(hours Day) bool {
	if len(hours) < 24 {
		return false
	}
	for _, w := range hours {
		if w.Temperature == nil {
			return false
		}
	}
	return true
}

func (c *Client) get(ctx context.Context, endpoint string, latitude, longitude float64, date string) (Day, error) {
	query := url.Values{
		"latitude":   {fmt.Sprintf("%.1f", latitude)},
		"longitude":  {fmt.Sprintf("%.1f", longitude)},
		"start_date": {date},
		"end_date":   {date},
		"hourly":     {strings.Join(variables, ",")},
		"timezone":   {"GMT"},
		"timeformat": {"unixtime"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", UserAgent)
	answer, err := c.http.Do(request)
	if err != nil {
		metrics.WeatherRequests.WithLabelValues("error").Inc()
		return nil, err
	}
	defer answer.Body.Close()
	if answer.StatusCode == http.StatusTooManyRequests {
		metrics.WeatherRequests.WithLabelValues("rate_limited").Inc()
		return nil, ErrRateLimited
	}
	var body response
	if err := json.NewDecoder(io.LimitReader(answer.Body, 1<<20)).Decode(&body); err != nil {
		metrics.WeatherRequests.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("open-meteo: %s: %w", answer.Status, err)
	}
	if answer.StatusCode != http.StatusOK {
		// a date out of the range of the API answers 400 with its reason
		if answer.StatusCode == http.StatusBadRequest {
			metrics.WeatherRequests.WithLabelValues("unavailable").Inc()
			return nil, fmt.Errorf("%w: %s", ErrUnavailable, body.Reason)
		}
		metrics.WeatherRequests.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("open-meteo: %s: %s", answer.Status, body.Reason)
	}
	metrics.WeatherRequests.WithLabelValues("ok").Inc()
	h := body.Hourly
	at := func(values []*float64, i int) *float64 {
		if i < len(values) {
			return values[i]
		}
		return nil
	}
	hours := Day{}
	for i, t := range h.Time {
		w := Weather{Time: time.Unix(t, 0).UTC(), Latitude: latitude, Longitude: longitude,
			Temperature: at(h.Temperature, i), ApparentTemperature: at(h.ApparentTemperature, i),
			Humidity: at(h.Humidity, i), Precipitation: at(h.Precipitation, i), CloudCover: at(h.CloudCover, i),
			WindSpeed: at(h.WindSpeed, i), WindGusts: at(h.WindGusts, i), WindDirection: at(h.WindDirection, i),
			Source: "open-meteo"}
		if code := at(h.Code, i); code != nil {
			c := int(*code)
			w.Code = &c
		}
		hours[w.Time] = w
	}
	if len(hours) == 0 {
		return nil, ErrUnavailable
	}
	return hours, nil
}

// At returns the weather of the hour nearest to t, its precipitation the one of the hour t is in (Open-Meteo
// sums it over the hour ending at its time); ErrUnavailable without temperature then
func (d Day) At(t time.Time) (Weather, error) {
	nearest := t.UTC().Round(time.Hour)
	w, ok := d[nearest]
	if !ok || w.Temperature == nil {
		return Weather{}, ErrUnavailable
	}
	if during, ok := d[t.UTC().Truncate(time.Hour).Add(time.Hour)]; ok {
		w.Precipitation = during.Precipitation
	}
	return w, nil
}

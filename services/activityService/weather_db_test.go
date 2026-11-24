package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/weather"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestBackfillWeather(t *testing.T) {
	db := emptyDB(t)
	user := models.Users{Username: "weather", Email: "weather@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	// a fake Open-Meteo: 12 °C at every hour, refusing the requests north of 60°
	var mu sync.Mutex
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RawQuery)
		mu.Unlock()
		q := r.URL.Query()
		if strings.HasPrefix(q.Get("latitude"), "6") {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error": true, "reason": "limit"}`)
			return
		}
		day, _ := time.Parse(time.DateOnly, q.Get("start_date"))
		var times, temps []string
		for h := 0; h < 24; h++ {
			times = append(times, fmt.Sprint(day.Add(time.Duration(h)*time.Hour).Unix()))
			temps = append(temps, "12")
		}
		fmt.Fprintf(w, `{"hourly": {"time": [%s], "temperature_2m": [%s], "wind_speed_10m": [%s]}}`,
			strings.Join(times, ","), strings.Join(temps, ","), strings.Join(temps, ","))
	}))
	defer server.Close()
	saved, savedPause := Weather, WeatherPause
	Weather, WeatherPause = weather.New(server.URL), time.Millisecond
	t.Cleanup(func() { Weather, WeatherPause = saved, savedPause })

	day := time.Date(2025, 6, 1, 7, 10, 0, 0, time.UTC)
	create := func(start *types.GpsPoint, date time.Time) uuid.UUID {
		t.Helper()
		a := models.Activity{ID: uuid.New(), UserID: user.ID, Title: "ride", Sport: "cycling", Date: date, StartPosition: start}
		if err := db.Omit("User", "Equipment").Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	// the morning and the evening rides from home: one request; a ride elsewhere: another
	morning := create(&types.GpsPoint{Lat: 48.8566, Lon: 2.3522}, day)
	evening := create(&types.GpsPoint{Lat: 48.8601, Lon: 2.3610}, day.Add(10*time.Hour))
	elsewhere := create(&types.GpsPoint{Lat: 45.76, Lon: 4.83}, day.AddDate(0, 0, -1))
	// no usable start: marked, never asked
	invalid := create(&types.GpsPoint{Lat: 180, Lon: 400}, day.AddDate(0, 0, -2))
	// indoors: not selected
	indoor := create(nil, day.AddDate(0, 0, -3))
	// the oldest, refused by the source: left for the next start
	refused := create(&types.GpsPoint{Lat: 60.17, Lon: 24.94}, day.AddDate(0, 0, -4))

	if err := BackfillWeather(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 {
		t.Errorf("%d requests, want 3 (home, elsewhere, refused): %v", len(asked), asked)
	}
	if !strings.Contains(asked[0], "latitude=48.9") || !strings.Contains(asked[0], "longitude=2.4") {
		t.Errorf("coordinates sent %s, want rounded", asked[0])
	}
	read := func(id uuid.UUID) *weather.Weather {
		t.Helper()
		a, err := db.GetActivity(id)
		if err != nil {
			t.Fatal(err)
		}
		return a.Weather
	}
	for name, id := range map[string]uuid.UUID{"morning": morning, "evening": evening, "elsewhere": elsewhere} {
		w := read(id)
		if w == nil || w.Unavailable || w.Temperature == nil || *w.Temperature != 12 || w.Source != "open-meteo" {
			t.Errorf("%s: %+v", name, w)
		}
	}
	if w := read(morning); !w.Time.Equal(time.Date(2025, 6, 1, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("morning hour %v", w.Time)
	}
	if w := read(evening); !w.Time.Equal(time.Date(2025, 6, 1, 17, 0, 0, 0, time.UTC)) {
		t.Errorf("evening hour %v", w.Time)
	}
	if w := read(invalid); w == nil || !w.Unavailable {
		t.Errorf("invalid start: %+v", w)
	}
	if read(indoor) != nil || read(refused) != nil {
		t.Errorf("indoor %+v, refused %+v: want none", read(indoor), read(refused))
	}

	// the next start asks for the one refused only
	asked = nil
	if err := BackfillWeather(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], "latitude=60.2") {
		t.Errorf("second run asked %v", asked)
	}

	// an edit of the activity (UpdateActivity saves the whole of it) keeps its weather
	edited, err := db.GetActivity(morning)
	if err != nil {
		t.Fatal(err)
	}
	edited.Title = "renamed"
	if err := db.UpdateActivity(edited); err != nil {
		t.Fatal(err)
	}
	if w := read(morning); w == nil || w.Temperature == nil {
		t.Errorf("weather after an edit: %+v", w)
	}

	// an upload: its weather at once
	asked = nil
	start := types.GpsPoint{Lat: 43.6, Lon: 1.44}
	uploaded := create(&start, day.AddDate(0, 0, 3))
	if err := FetchActivityWeather(context.Background(), db, uploaded, day.AddDate(0, 0, 3), &start); err != nil {
		t.Fatal(err)
	}
	if w := read(uploaded); w == nil || w.Temperature == nil || len(asked) != 1 {
		t.Errorf("uploaded: %+v, %d requests", w, len(asked))
	}
	// without source: nothing asked, nothing saved
	Weather = nil
	other := create(&start, day.AddDate(0, 0, 4))
	if err := FetchActivityWeather(context.Background(), db, other, day.AddDate(0, 0, 4), &start); err != nil || read(other) != nil {
		t.Errorf("without source: %v %+v", err, read(other))
	}
}

package weather

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// a fake Open-Meteo: the archive knows the hours before archiveUntil, the forecast API all of them
func fakeServer(t *testing.T, archiveUntil time.Time) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		q := r.URL.Query()
		if q.Get("latitude") == "0.0" {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error": true, "reason": "Hourly API request limit exceeded"}`)
			return
		}
		day, err := time.Parse(time.DateOnly, q.Get("start_date"))
		if err != nil || q.Get("end_date") != q.Get("start_date") || q.Get("timezone") != "GMT" ||
			q.Get("timeformat") != "unixtime" || len(strings.Split(q.Get("hourly"), ",")) > 10 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error": true, "reason": "bad request"}`)
			return
		}
		var times, temps, precips, codes []string
		for h := 0; h < 24; h++ {
			at := day.Add(time.Duration(h) * time.Hour)
			times = append(times, fmt.Sprint(at.Unix()))
			temp := fmt.Sprintf("%d.5", h)
			if r.URL.Path == "/v1/archive" && !at.Before(archiveUntil) {
				temp = "null"
			}
			temps = append(temps, temp)
			precips = append(precips, fmt.Sprintf("0.%d", h%10))
			codes = append(codes, "61")
		}
		fmt.Fprintf(w, `{"hourly": {"time": [%s], "temperature_2m": [%s], "apparent_temperature": [%s],
			"precipitation": [%s], "weather_code": [%s], "wind_speed_10m": [%s], "wind_direction_10m": [%s]}}`,
			strings.Join(times, ","), strings.Join(temps, ","), strings.Join(temps, ","),
			strings.Join(precips, ","), strings.Join(codes, ","), strings.Join(temps, ","), strings.Join(temps, ","))
	}))
	t.Cleanup(server.Close)
	return server, &asked
}

func TestDay(t *testing.T) {
	now := time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)
	server, asked := fakeServer(t, now.Add(-30*time.Hour))
	c := New(server.URL)
	c.now = func() time.Time { return now }

	// an old ride: the archive only, the coordinates rounded
	start := time.Date(2026, 6, 1, 14, 20, 0, 0, time.UTC)
	day, err := c.Day(context.Background(), 48.8566, 2.3522, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 || !strings.HasPrefix((*asked)[0], "/v1/archive?") ||
		!strings.Contains((*asked)[0], "latitude=48.9") || !strings.Contains((*asked)[0], "longitude=2.4") {
		t.Fatalf("asked %v", *asked)
	}
	w, err := day.At(start)
	if err != nil {
		t.Fatal(err)
	}
	// 14:20: the values of 14:00, the precipitation of 14:00 to 15:00 (at 15:00)
	if !w.Time.Equal(time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC)) || *w.Temperature != 14.5 ||
		*w.Precipitation != 0.5 || *w.Code != 61 || w.Humidity != nil || w.Source != "open-meteo" ||
		w.Latitude != 48.9 || w.Longitude != 2.4 {
		t.Errorf("weather %+v", w)
	}
	// 14:40: the hour nearest is 15:00
	if w, _ := day.At(start.Add(20 * time.Minute)); *w.Temperature != 15.5 {
		t.Errorf("14:40: %v", *w.Temperature)
	}

	// yesterday: the archive lacks the last hours, the forecast API gives them
	*asked = nil
	yesterday := now.Add(-26 * time.Hour)
	day, err = c.Day(context.Background(), 45, 5, yesterday)
	if err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 2 || !strings.HasPrefix((*asked)[1], "/v1/forecast?") {
		t.Fatalf("asked %v", *asked)
	}
	if w, err := day.At(yesterday); err != nil || *w.Temperature != float64(yesterday.Hour())+0.5 {
		t.Errorf("yesterday: %+v %v", w, err)
	}

	// refused: too many requests
	if _, err := c.Day(context.Background(), 0.01, 0, start); !errors.Is(err, ErrRateLimited) {
		t.Errorf("rate limited: %v", err)
	}
	// an hour without temperature
	if _, err := (Day{}).At(start); !errors.Is(err, ErrUnavailable) {
		t.Errorf("empty day: %v", err)
	}
}

func TestNew(t *testing.T) {
	if New("none") != nil {
		t.Error("none: a client")
	}
	if c := New(""); c.archive != ArchiveURL || c.forecast != ForecastURL {
		t.Errorf("default %+v", c)
	}
	if c := New("https://weather.example.com/ "); c.archive != "https://weather.example.com/v1/archive" ||
		c.forecast != "https://weather.example.com/v1/forecast" {
		t.Errorf("self-hosted %+v", c)
	}
	for degrees, want := range map[float64]float64{48.8566: 48.9, 2.3522: 2.4, -0.04: 0, 45.06: 45.1} {
		if got := Round(degrees); got != want || fmt.Sprintf("%.1f", got) != fmt.Sprintf("%.1f", want) {
			t.Errorf("Round(%v) = %v, want %v", degrees, got, want)
		}
	}
}

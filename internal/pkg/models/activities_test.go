package models

import (
	"MySportWeb/internal/pkg/types"
	"MySportWeb/internal/pkg/weather"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestHidePrivate(t *testing.T) {
	home := types.GpsPoint{Lat: 45.123456, Lon: 5.654321}
	equipmentID, routeID := uuid.New(), uuid.New()
	a := Activity{
		User:             Users{Username: "owner", Email: "owner@example.com"},
		UserID:           42,
		Filename:         "ride.fit",
		FilePath:         "MEDIA/0b5d3e0e-3c4a-4d9f-9a63-0f0d7c1b2a11/Activities/ride.fit",
		GpsPoints:        types.GpsPointArray{home, {Lat: 45.2, Lon: 5.7}, {Lat: 45.3, Lon: 5.8}},
		Lats:             types.FloatArray{home.Lat, 45.2, 45.3},
		Lons:             types.FloatArray{home.Lon, 5.7, 5.8},
		StartPosition:    &home,
		EndPosition:      &home,
		MaxSpeedPosition: home,
		GpsCenter:        &home,
		GpsBounds:        types.GpsPointArray{home, home},
		PublicGpsPoints:  types.GpsPointArray{{Lat: 45.2, Lon: 5.7}, {Lat: 45.3, Lon: 5.9}},
		EquipmentID:      &equipmentID,
		RouteID:          &routeID,
		Serialnumber:     "3999999999",
		Device:           types.Device{Model: "Edge 530", Serial: "3999999999"},
		Title:            "Morning ride",
		Distance:         42000,
		RadarEvents: types.RadarEventArray{{Offset: 120, Duration: 9, Threat: types.RadarFast, Vehicles: 2,
			Distance: 7, Position: &types.GpsPoint{Lat: 45.987654, Lon: 5.456789}}},
	}
	temperature := 12.5
	a.Weather = &weather.Weather{Latitude: 45.1, Longitude: 5.7, Temperature: &temperature, Source: "open-meteo"}
	stored := a.Weather
	a.HidePrivate()

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	// the radar events tell where and when the owner rides in traffic: theirs only
	for _, secret := range []string{"45.123456", "5.654321", "ride.fit", "0b5d3e0e", "owner", "3999999999",
		equipmentID.String(), routeID.String(), "45.987654", "radar_events\":[", "\"threat\""} {
		if strings.Contains(string(data), secret) {
			t.Errorf("%s still in %s", secret, data)
		}
	}
	if a.UserID != 0 {
		t.Errorf("user id %d", a.UserID)
	}
	// the weather without the place it was asked for; the activity's own value untouched
	if a.Weather == nil || a.Weather.Latitude != 0 || a.Weather.Longitude != 0 || *a.Weather.Temperature != 12.5 ||
		stored.Latitude != 45.1 {
		t.Errorf("weather %+v (stored %+v)", a.Weather, stored)
	}
	// what the others see is kept
	if a.Title != "Morning ride" || a.Distance != 42000 || len(a.PublicGpsPoints) != 2 || a.Device.Model != "Edge 530" {
		t.Errorf("public fields changed: %+v", a)
	}
	// the map is centered on the public trace
	want := types.GpsPointArray{{Lat: 45.2, Lon: 5.7}, {Lat: 45.3, Lon: 5.9}}
	if len(a.GpsBounds) != 2 || a.GpsBounds[0] != want[0] || a.GpsBounds[1] != want[1] {
		t.Errorf("bounds %v, want %v", a.GpsBounds, want)
	}
	if a.GpsCenter == nil || a.GpsCenter.Lat < 45.24 || a.GpsCenter.Lat > 45.26 {
		t.Errorf("center %v", a.GpsCenter)
	}

	empty := Activity{GpsCenter: &home, GpsBounds: types.GpsPointArray{home, home}}
	empty.HidePrivate()
	if empty.GpsCenter != nil || empty.GpsBounds != nil {
		t.Errorf("without public trace: center %v bounds %v", empty.GpsCenter, empty.GpsBounds)
	}
}

package models

import (
	"testing"
	"time"
)

func TestHealthDataValidate(t *testing.T) {
	date := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		data  HealthData
		valid bool
	}{
		{"weight only", HealthData{Date: date, Weight: 72.4}, true},
		{"blood pressure only", HealthData{Date: date, Systolic: 120, Diastolic: 80}, true},
		{"everything", HealthData{Date: date, Weight: 72, Fat: 15, Muscle: 40, Bone: 3, BodyWater: 55,
			RestHR: 48, Vo2Max: 55, Systolic: 118, Diastolic: 76}, true},
		{"no date", HealthData{Weight: 72}, false},
		{"nothing measured", HealthData{Date: date}, false},
		{"negative weight", HealthData{Date: date, Weight: -1}, false},
		{"percentage above 100", HealthData{Date: date, Fat: 120}, false},
		{"systolic without diastolic", HealthData{Date: date, Systolic: 120}, false},
		{"pressures swapped", HealthData{Date: date, Systolic: 80, Diastolic: 120}, false},
		{"heart rate out of range", HealthData{Date: date, RestHR: 400}, false},
	} {
		if err := test.data.Validate(); (err == nil) != test.valid {
			t.Errorf("%s: %v, want valid %v", test.name, err, test.valid)
		}
	}
}

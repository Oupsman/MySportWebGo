package models

import (
	"os"
	"strings"
	"testing"
	"time"
)

func healthTestDB(t *testing.T) *DB {
	dsn := os.Getenv("MYSPORTWEB_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSPORTWEB_TEST_DSN is not set")
	}
	db, err := ConnectToDB("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "test") {
		t.Fatalf("refusing to empty database %q, its name must contain \"test\"", name)
	}
	if err := CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("TRUNCATE health_data, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestImportHealthDatas(t *testing.T) {
	db := healthTestDB(t)
	user := Users{Username: "health", Email: "health@test.local"}
	other := Users{Username: "other", Email: "other@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 1, 7, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	// another user's measure at the same date stays apart
	if _, err := db.ImportHealthDatas(other.ID, []HealthData{{Date: day, Weight: 90}}); err != nil {
		t.Fatal(err)
	}

	first := []HealthData{
		{Date: day, Weight: 72.4},
		{Date: day.Add(24 * time.Hour), Weight: 72.1, Fat: 15},
		// a second value at the same date, in the same import: one measure
		{Date: day.Add(24 * time.Hour), RestHR: 48},
	}
	result, err := db.ImportHealthDatas(user.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if result != (HealthImport{Created: 2, Merged: 1}) {
		t.Errorf("first import: %+v", result)
	}

	// the same import again: nothing changes
	result, err = db.ImportHealthDatas(user.ID, first)
	if err != nil || result != (HealthImport{Unchanged: 3}) {
		t.Errorf("same import again: %+v, %v", result, err)
	}

	// the same date in UTC, another weight and a blood pressure: the pressure completes the measure,
	// the weight already measured is kept
	result, err = db.ImportHealthDatas(user.ID, []HealthData{{Date: day.UTC(), Weight: 80, Systolic: 120, Diastolic: 80}})
	if err != nil || result != (HealthImport{Merged: 1}) {
		t.Errorf("completing import: %+v, %v", result, err)
	}

	measures, err := db.GetHealthDatas(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(measures) != 2 {
		t.Fatalf("%d measures, want 2", len(measures))
	}
	// newest first
	if m := measures[0]; m.Weight != 72.1 || m.Fat != 15 || m.RestHR != 48 {
		t.Errorf("second day: %+v", m)
	}
	if m := measures[1]; m.Weight != 72.4 || m.Systolic != 120 || m.Diastolic != 80 {
		t.Errorf("first day: %+v", m)
	}
}

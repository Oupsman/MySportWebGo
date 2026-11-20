package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestDeviceSearch(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "devices", Email: "devices@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID), IsDefault: true}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}
	create := func(name string) models.Activity {
		t.Helper()
		activity := importFit(t, name, user, equipment)
		if err := CreateActivity(db, &activity, nil); err != nil {
			t.Fatal(err)
		}
		return activity
	}
	karoo := create(sameRouteFiles[0])
	karoo2 := create(sameRouteFiles[1])
	fenix := create("2022-08-30-18-12-37")
	if karoo.DeviceName != "Hammerhead Karoo 2" || fenix.DeviceName != "Garmin Fenix 6X" {
		t.Fatalf("devices %q and %q", karoo.DeviceName, fenix.DeviceName)
	}
	// the import creates the device at its first activity, and links the next ones to it
	if karoo.DeviceID == nil || fenix.DeviceID == nil || karoo2.DeviceID == nil ||
		*karoo2.DeviceID != *karoo.DeviceID || *fenix.DeviceID == *karoo.DeviceID {
		t.Errorf("device ids %v, %v and %v", karoo.DeviceID, karoo2.DeviceID, fenix.DeviceID)
	}
	// entered by hand: no device
	manual := models.Activity{UserID: user.ID, Sport: "running", Title: "manual", Date: time.Now()}
	if err := db.Omit("User", "Equipment").Create(&manual).Error; err != nil {
		t.Fatal(err)
	}

	search := func(filter models.ActivityFilter) int {
		t.Helper()
		result, err := db.SearchActivities(user.ID, filter, "date", true, 1, 25, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		return len(result.Activities)
	}
	if n := search(models.ActivityFilter{Device: "Hammerhead Karoo 2"}); n != 2 {
		t.Errorf("%d Karoo 2 activities, want 2", n)
	}
	if n := search(models.ActivityFilter{Device: "Garmin Fenix 6X"}); n != 1 {
		t.Errorf("%d Fenix activities, want 1", n)
	}
	if n := search(models.ActivityFilter{NoDevice: true}); n != 1 {
		t.Errorf("%d activities without device, want 1", n)
	}

	result, err := db.SearchActivities(user.ID, models.ActivityFilter{}, "date", true, 1, 25, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	// the most used first, no entry for the activity without device
	if len(result.Devices) != 2 || result.Devices[0].Name != "Hammerhead Karoo 2" || result.Devices[0].Activities != 2 ||
		result.Devices[1].Name != "Garmin Fenix 6X" {
		t.Errorf("devices %+v", result.Devices)
	}
	for _, row := range result.Activities {
		if row.ID == fenix.ID && row.DeviceName != "Garmin Fenix 6X" {
			t.Errorf("row device %q", row.DeviceName)
		}
	}
}

// the activities imported before the devices were read get theirs from their FIT file
func TestBackfillDevice(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "olddevice", Email: "olddevice@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := models.Equipments{Name: "bike", UserID: int(user.ID), IsDefault: true}
	if err := db.Create(&equipment).Error; err != nil {
		t.Fatal(err)
	}
	old := importFit(t, "2022-08-30-18-12-37", user, equipment)
	old.FilePath = "../../prototypes/2022-08-30-18-12-37.fit"
	old.EngineVersion = 2
	if err := db.CreateActivity(&old); err != nil {
		t.Fatal(err)
	}
	// as the column is for the activities imported before it existed
	if err := db.Exec("UPDATE activities SET device_name = NULL, device = '{}' WHERE id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}

	if err := BackfillTimeStamps(context.Background(), db, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetActivity(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EngineVersion != vars.Engine_version || got.DeviceName != "Garmin Fenix 6X" || got.Device.Software != "22.10" {
		t.Errorf("not re-analysed: engine %d, device %q %+v", got.EngineVersion, got.DeviceName, got.Device)
	}
}

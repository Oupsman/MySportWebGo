package models

import (
	"MySportWeb/internal/pkg/types"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func devicesTestDB(t *testing.T) (*DB, Users, Users) {
	db := healthTestDB(t)
	if err := db.Exec("TRUNCATE devices, activities, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := Users{Username: "devices", Email: "devices@test.local"}
	other := Users{Username: "other", Email: "other@test.local"}
	for _, u := range []*Users{&user, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, user, other
}

func TestFindOrCreateDevice(t *testing.T) {
	db, user, other := devicesTestDB(t)
	find := func(userID uint, device types.Device, correction string) uuid.UUID {
		t.Helper()
		id, err := db.FindOrCreateDevice(userID, device, "Garmin "+device.Model, correction)
		if err != nil {
			t.Fatal(err)
		}
		if id == nil {
			t.Fatalf("no device for %+v", device)
		}
		return *id
	}
	edge := types.Device{Brand: "Garmin", Model: "Edge 530", Serial: "3999999999", Software: "9.10"}
	first := find(user.ID, edge, AltitudeCorrectionReplace)

	// the same unit, after a firmware update: the device of its first activity, with its correction
	updated := edge
	updated.Software = "9.20"
	if id := find(user.ID, updated, AltitudeCorrectionNone); id != first {
		t.Errorf("same unit: device %s, want %s", id, first)
	}
	var device Devices
	if err := db.First(&device, "id = ?", first).Error; err != nil {
		t.Fatal(err)
	}
	if device.AltitudeCorrection != AltitudeCorrectionReplace || device.Name != "Garmin Edge 530" {
		t.Errorf("device %+v", device)
	}

	// another unit of the same model, a unit without serial number, the same unit of another user:
	// each its own device
	secondUnit := edge
	secondUnit.Serial = "3888888888"
	noSerial := edge
	noSerial.Serial = ""
	ids := map[uuid.UUID]string{first: "first unit"}
	for name, id := range map[string]uuid.UUID{
		"second unit":         find(user.ID, secondUnit, AltitudeCorrectionNone),
		"unit without serial": find(user.ID, noSerial, AltitudeCorrectionNone),
		"other user":          find(other.ID, edge, AltitudeCorrectionNone),
		"other model":         find(user.ID, types.Device{Brand: "Garmin", Model: "Fenix 6X", Serial: edge.Serial}, ""),
	} {
		if previous, found := ids[id]; found {
			t.Errorf("%s has the device of the %s", name, previous)
		}
		ids[id] = name
	}
	// every unit without serial number of a model shares its device
	if id := find(user.ID, noSerial, AltitudeCorrectionNone); ids[id] != "unit without serial" {
		t.Errorf("unit without serial: device of the %q", ids[id])
	}

	// a file not naming its device
	if id, err := db.FindOrCreateDevice(user.ID, types.Device{}, "", AltitudeCorrectionNone); err != nil || id != nil {
		t.Errorf("unnamed device: %v, %v", id, err)
	}
	// an unknown correction is none
	var fenix Devices
	if err := db.First(&fenix, "model = ?", "Fenix 6X").Error; err != nil {
		t.Fatal(err)
	}
	if fenix.AltitudeCorrection != AltitudeCorrectionNone {
		t.Errorf("correction %q, want none", fenix.AltitudeCorrection)
	}
}

func TestBackfillDevices(t *testing.T) {
	db, user, other := devicesTestDB(t)
	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	create := func(userID uint, device types.Device, name string, daysLater int) *Activity {
		t.Helper()
		activity := Activity{UserID: userID, Title: name, Sport: "cycling", Device: device, DeviceName: name,
			Date: day.AddDate(0, 0, daysLater)}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
		return &activity
	}
	edge := types.Device{Brand: "Garmin", Model: "Edge 530", Serial: "3999999999"}
	// an older engine named it differently: the device takes the name of the latest activity
	old := create(user.ID, edge, "Garmin edge530", 0)
	recent := create(user.ID, edge, "Garmin Edge 530", 1)
	otherUnit := create(user.ID, types.Device{Brand: "Garmin", Model: "Edge 530", Serial: "3888888888"}, "Garmin Edge 530", 2)
	karoo := create(user.ID, types.Device{Brand: "Hammerhead", Model: "Karoo 2"}, "Hammerhead Karoo 2", 3)
	otherUser := create(other.ID, edge, "Garmin Edge 530", 0)
	unnamed := create(user.ID, types.Device{}, "", 4)
	// a device already created, whose correction the backfill keeps
	fenix := types.Device{Brand: "Garmin", Model: "Fenix 6X", Serial: "1234"}
	fenixID, err := db.FindOrCreateDevice(user.ID, fenix, "Garmin Fenix 6X", AltitudeCorrectionCalibrate)
	if err != nil {
		t.Fatal(err)
	}
	fenixActivity := create(user.ID, fenix, "Garmin Fenix 6X", 5)

	linked, err := db.BackfillDevices()
	if err != nil {
		t.Fatal(err)
	}
	if linked != 6 {
		t.Errorf("%d activities linked, want 6", linked)
	}
	deviceOf := func(activity *Activity) *uuid.UUID {
		t.Helper()
		var stored Activity
		if err := db.Select("device_id").First(&stored, "id = ?", activity.ID).Error; err != nil {
			t.Fatal(err)
		}
		return stored.DeviceID
	}
	if deviceOf(unnamed) != nil {
		t.Error("activity without device linked to one")
	}
	if id := deviceOf(fenixActivity); id == nil || *id != *fenixID {
		t.Errorf("Fenix activity on device %v, want %s", id, fenixID)
	}
	same := deviceOf(old)
	if same == nil || deviceOf(recent) == nil || *deviceOf(recent) != *same {
		t.Fatal("the activities of a unit are on different devices")
	}
	for name, activity := range map[string]*Activity{"other unit": otherUnit, "Karoo": karoo, "other user": otherUser} {
		if id := deviceOf(activity); id == nil || *id == *same {
			t.Errorf("%s on device %v", name, id)
		}
	}

	var devices []Devices
	if err := db.Order("user_id, model, serial").Find(&devices).Error; err != nil {
		t.Fatal(err)
	}
	if len(devices) != 5 {
		t.Fatalf("%d devices, want 5: %+v", len(devices), devices)
	}
	for _, device := range devices {
		if device.ID == *same && device.Name != "Garmin Edge 530" {
			t.Errorf("device named %q, want the name of its latest activity", device.Name)
		}
		if device.ID == *fenixID && device.AltitudeCorrection != AltitudeCorrectionCalibrate {
			t.Errorf("Fenix correction %q, want calibrate", device.AltitudeCorrection)
		}
		if device.Model == "Karoo 2" && device.Serial != "" {
			t.Errorf("Karoo serial %q", device.Serial)
		}
	}

	// done once
	if linked, err := db.BackfillDevices(); err != nil || linked != 0 {
		t.Errorf("second backfill: %d linked, %v", linked, err)
	}
}

func TestDevicesStats(t *testing.T) {
	db, user, other := devicesTestDB(t)
	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	link := func(userID uint, device types.Device, name string, daysLater int) uuid.UUID {
		t.Helper()
		id, err := db.FindOrCreateDevice(userID, device, name, AltitudeCorrectionNone)
		if err != nil {
			t.Fatal(err)
		}
		activity := Activity{UserID: userID, Title: name, Sport: "cycling", Device: device, DeviceName: name,
			DeviceID: id, Date: day.AddDate(0, 0, daysLater)}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
		return *id
	}
	edge := types.Device{Brand: "Garmin", Model: "Edge 530", Serial: "3999999999", Software: "9.10"}
	first := link(user.ID, edge, "Garmin Edge 530", 0)
	edge.Software = "9.20"
	link(user.ID, edge, "Garmin Edge 530", 2)
	second := link(user.ID, types.Device{Brand: "Garmin", Model: "Edge 530", Serial: "3888888888"}, "Garmin Edge 530", 1)
	karoo := link(user.ID, types.Device{Brand: "Hammerhead", Model: "Karoo 2"}, "Hammerhead Karoo 2", 5)
	theirs := link(other.ID, edge, "Garmin Edge 530", 3)

	devices, err := db.GetDevicesStats(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// the latest used first, none of the other user
	if len(devices) != 3 || devices[0].ID != karoo || devices[1].ID != first || devices[2].ID != second {
		t.Fatalf("devices %+v", devices)
	}
	if devices[1].Activities != 2 || devices[1].Software != "9.20" || !devices[1].LastUsed.Equal(day.AddDate(0, 0, 2)) {
		t.Errorf("first unit: %d activities, firmware %q, last used %v", devices[1].Activities, devices[1].Software,
			devices[1].LastUsed)
	}
	if devices[0].Software != "" {
		t.Errorf("Karoo firmware %q, want none", devices[0].Software)
	}
	if _, err := db.GetDeviceStats(user.ID, theirs); err != gorm.ErrRecordNotFound {
		t.Errorf("device of the other user: %v", err)
	}

	if err := db.SetAltitudeCorrection(user.ID, second, AltitudeCorrectionReplace); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAltitudeCorrection(user.ID, theirs, AltitudeCorrectionReplace); err != gorm.ErrRecordNotFound {
		t.Errorf("correction of the other user's device: %v", err)
	}
	for id, want := range map[uuid.UUID]string{second: AltitudeCorrectionReplace, first: AltitudeCorrectionNone,
		theirs: AltitudeCorrectionNone} {
		var device Devices
		if err := db.First(&device, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if device.AltitudeCorrection != want {
			t.Errorf("device %s: correction %q, want %q", id, device.AltitudeCorrection, want)
		}
	}

	// the search tells the units apart, and filters on one
	search, err := db.SearchActivities(user.ID, ActivityFilter{DeviceID: &first}, "date", true, 1, 25, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if search.Total != 2 {
		t.Errorf("%d activities of the first unit, want 2", search.Total)
	}
	if len(search.Devices) != 3 || search.Devices[0].ID != first || search.Devices[0].Serial != "3999999999" ||
		search.Devices[0].Activities != 2 {
		t.Errorf("search devices %+v", search.Devices)
	}
	byName, err := db.SearchActivities(user.ID, ActivityFilter{Device: "Garmin Edge 530"}, "date", true, 1, 25, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if byName.Total != 3 {
		t.Errorf("%d activities named Garmin Edge 530, want both units' 3", byName.Total)
	}
}

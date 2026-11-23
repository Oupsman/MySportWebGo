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

func TestMergeDevices(t *testing.T) {
	db, user, other := devicesTestDB(t)
	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	activities := 0
	record := func(userID uint, device types.Device, name string) uuid.UUID {
		t.Helper()
		id, err := db.FindOrCreateDevice(userID, device, name, AltitudeCorrectionNone)
		if err != nil {
			t.Fatal(err)
		}
		activities++
		activity := Activity{UserID: userID, Title: name, Sport: "cycling", Device: device, DeviceName: name,
			DeviceID: id, Date: day.AddDate(0, 0, activities)}
		if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
			t.Fatal(err)
		}
		return *id
	}
	// a Karoo 2 whose files gave no serial number, then one after a firmware update
	before := types.Device{Brand: "Hammerhead", Model: "Karoo 2"}
	after := types.Device{Brand: "Hammerhead", Model: "Karoo 2", Serial: "K2-0042"}
	old := record(user.ID, before, "Hammerhead Karoo 2")
	record(user.ID, before, "Hammerhead Karoo 2")
	karoo := record(user.ID, after, "Hammerhead Karoo 2")
	edge := record(user.ID, types.Device{Brand: "Garmin", Model: "Edge 530", Serial: "1"}, "Garmin Edge 530")
	theirs := record(other.ID, after, "Hammerhead Karoo 2")
	if err := db.SetAltitudeCorrection(user.ID, karoo, AltitudeCorrectionReplace); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct {
		id, into uuid.UUID
		want     error
	}{
		"into itself":         {old, old, ErrMergeDevice},
		"into another user's": {old, theirs, gorm.ErrRecordNotFound},
		"another user's":      {theirs, karoo, gorm.ErrRecordNotFound},
		"unknown":             {uuid.New(), karoo, gorm.ErrRecordNotFound},
	} {
		if err := db.MergeDevices(user.ID, c.id, c.into); err != c.want {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}

	if err := db.MergeDevices(user.ID, old, karoo); err != nil {
		t.Fatal(err)
	}
	devices, err := db.GetDevicesStats(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	// the merged device is gone from the list, its activities and its name on the other one
	if len(devices) != 2 || devices[1].ID != karoo || devices[1].Activities != 3 || len(devices[1].Aliases) != 1 ||
		devices[1].Aliases[0].Serial != "" || devices[1].Aliases[0].Name != "Hammerhead Karoo 2" {
		t.Fatalf("devices after the merge %+v", devices)
	}
	if devices[0].ID != edge || len(devices[0].Aliases) != 0 {
		t.Errorf("Edge %+v", devices[0])
	}
	if _, err := db.GetDeviceStats(user.ID, old); err != gorm.ErrRecordNotFound {
		t.Errorf("merged device read: %v", err)
	}
	if err := db.SetAltitudeCorrection(user.ID, old, AltitudeCorrectionNone); err != gorm.ErrRecordNotFound {
		t.Errorf("correction of the merged device: %v", err)
	}
	if err := db.MergeDevices(user.ID, old, edge); err != gorm.ErrRecordNotFound {
		t.Errorf("merging a merged device again: %v", err)
	}

	// a file named as before goes to the device merged into, with its correction
	if id, err := db.FindOrCreateDevice(user.ID, before, "Hammerhead Karoo 2", AltitudeCorrectionNone); err != nil || *id != karoo {
		t.Errorf("import as before the merge: device %v, %v", id, err)
	}
	if correction, err := db.DeviceAltitudeCorrection(user.ID, before); err != nil || correction != AltitudeCorrectionReplace {
		t.Errorf("correction as before the merge: %q, %v", correction, err)
	}
	// and so does an activity imported before the devices
	unlinked := Activity{UserID: user.ID, Title: "old", Sport: "cycling", Device: before, DeviceName: "Hammerhead Karoo 2",
		Date: day}
	if err := db.Omit("User", "Equipment").Create(&unlinked).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := db.BackfillDevices(); err != nil {
		t.Fatal(err)
	}
	var linked Activity
	if err := db.Select("device_id").First(&linked, "id = ?", unlinked.ID).Error; err != nil {
		t.Fatal(err)
	}
	if linked.DeviceID == nil || *linked.DeviceID != karoo {
		t.Errorf("backfilled activity on device %v, want %s", linked.DeviceID, karoo)
	}

	// merged again: its aliases follow it, every name goes to the last device
	if err := db.MergeDevices(user.ID, karoo, edge); err != nil {
		t.Fatal(err)
	}
	devices, err = db.GetDevicesStats(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != edge || devices[0].Activities != 5 || len(devices[0].Aliases) != 2 {
		t.Fatalf("devices after the second merge %+v", devices)
	}
	for _, device := range []types.Device{before, after} {
		if id, err := db.FindOrCreateDevice(user.ID, device, "Hammerhead Karoo 2", AltitudeCorrectionNone); err != nil || *id != edge {
			t.Errorf("import of %+v: device %v, %v", device, id, err)
		}
	}
	// the other user's devices are untouched
	if mine, err := db.GetDevicesStats(other.ID); err != nil || len(mine) != 1 || mine[0].ID != theirs || mine[0].Activities != 1 {
		t.Errorf("other user's devices %+v, %v", mine, err)
	}
}

func TestDeviceLabel(t *testing.T) {
	db, user, other := devicesTestDB(t)
	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	karoo := types.Device{Brand: "Hammerhead", Model: "Karoo 3"}
	id, err := db.FindOrCreateDevice(user.ID, karoo, "Karoo", AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := db.FindOrCreateDevice(other.ID, karoo, "Karoo", AltitudeCorrectionNone)
	if err != nil {
		t.Fatal(err)
	}
	activity := Activity{UserID: user.ID, Title: "ride", Sport: "cycling", Device: karoo, DeviceName: "Karoo",
		DeviceID: id, Date: day}
	if err := db.Omit("User", "Equipment").Create(&activity).Error; err != nil {
		t.Fatal(err)
	}

	label := "Karoo 3"
	if err := db.UpdateDevice(user.ID, *id, &label, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateDevice(other.ID, *theirs, &label, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateDevice(other.ID, *id, &label, nil); err != gorm.ErrRecordNotFound {
		t.Errorf("label of the other user's device: %v", err)
	}
	// a correction alone keeps the label, and the label the correction
	if err := db.SetAltitudeCorrection(user.ID, *id, AltitudeCorrectionReplace); err != nil {
		t.Fatal(err)
	}
	device, err := db.GetDeviceStats(user.ID, *id)
	if err != nil {
		t.Fatal(err)
	}
	if device.Label != "Karoo 3" || device.Name != "Karoo" || device.AltitudeCorrection != AltitudeCorrectionReplace ||
		device.DisplayName() != "Karoo 3" {
		t.Errorf("device %+v", device.Devices)
	}
	// the next import finds the device without renaming it
	again, err := db.FindOrCreateDevice(user.ID, karoo, "Karoo", AltitudeCorrectionNone)
	if err != nil || *again != *id {
		t.Fatalf("device %v, %v", again, err)
	}
	if got, _ := db.DeviceLabel(*id); got != "Karoo 3" {
		t.Errorf("label after an import %q", got)
	}

	// the search shows the label, and still filters on the name of the files
	search, err := db.SearchActivities(user.ID, ActivityFilter{Device: "Karoo"}, "date", true, 1, 25, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if search.Total != 1 || search.Activities[0].DeviceName != "Karoo 3" {
		t.Errorf("search %d activities %+v", search.Total, search.Activities)
	}
	if len(search.Devices) != 1 || search.Devices[0].Name != "Karoo 3" {
		t.Errorf("search devices %+v", search.Devices)
	}

	// back to the name of the files
	empty := ""
	if err := db.UpdateDevice(user.ID, *id, &empty, nil); err != nil {
		t.Fatal(err)
	}
	search, err = db.SearchActivities(user.ID, ActivityFilter{}, "date", true, 1, 25, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if search.Activities[0].DeviceName != "Karoo" || search.Devices[0].Name != "Karoo" {
		t.Errorf("without label: %+v, %+v", search.Activities, search.Devices)
	}
}

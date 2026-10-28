package ui

import (
	"archive/zip"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"MySportWebSync/internal/config"
	"MySportWebSync/internal/device"
	"MySportWebSync/internal/health"
	"MySportWebSync/internal/state"
	"MySportWebSync/internal/syncer"
)

// capture saves the window in $MSW_UI_SHOTS when set, to look at the layout
func capture(t *testing.T, w fyne.Window, name string) {
	dir := os.Getenv("MSW_UI_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, w.Canvas().Capture())
}

func TestRender(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	u := New(a)
	m := syncer.NewManager(config.Default(), st, device.NewMTPScanner(), u.Changed, u.Notify)
	u.Attach(m)
	u.window.Resize(fyne.NewSize(640, 460))

	// without token, the settings come first
	if u.tabs.Selected().Text != "Settings" {
		t.Errorf("tab %d selected, want the settings", u.tabs.SelectedIndex())
	}
	capture(t, u.window, "settings")
	u.tabs.SelectIndex(0)
	capture(t, u.window, "devices-empty")

	cards := container.NewVBox(
		u.deviceCard(syncer.DeviceStatus{ID: "a", Name: "Forerunner 255", Kind: device.MTP, Syncing: true, Done: 3, Total: 8}),
		u.deviceCard(syncer.DeviceStatus{ID: "b", Name: "Edge 530", Kind: device.MassStorage,
			Last: "2 uploaded, 14 already on the server, 0 refused"}),
		u.deviceCard(syncer.DeviceStatus{ID: "c", Name: "fenix 7", Kind: device.MTP, LastErr: true,
			Last: "the server refused the device token: unauthorized: expired device token, check the token in the settings"}),
	)
	w := a.NewWindow("cards")
	w.SetContent(cards)
	w.Resize(fyne.NewSize(640, 560))
	capture(t, w, "devices")
}

const appleXML = `<?xml version="1.0" encoding="UTF-8"?>
<HealthData locale="en_US">
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Withings" unit="kg" startDate="2024-03-01 07:30:00 +0100" endDate="2024-03-01 07:30:00 +0100" value="74.2"/>
 <Record type="HKQuantityTypeIdentifierBodyFatPercentage" sourceName="Withings" unit="%" startDate="2024-03-01 07:30:00 +0100" endDate="2024-03-01 07:30:00 +0100" value="0.19"/>
 <Record type="HKQuantityTypeIdentifierBodyMass" sourceName="Withings" unit="kg" startDate="2026-10-20 07:30:00 +0200" endDate="2026-10-20 07:30:00 +0200" value="72.1"/>
 <Record type="HKQuantityTypeIdentifierRestingHeartRate" sourceName="Apple Watch" unit="count/min" startDate="2026-10-20 00:00:00 +0200" endDate="2026-10-20 23:59:00 +0200" value="47"/>
 <Record type="HKQuantityTypeIdentifierStepCount" sourceName="iPhone" unit="count" startDate="2026-10-20 08:00:00 +0200" endDate="2026-10-20 08:10:00 +0200" value="812"/>
 <Record type="HKQuantityTypeIdentifierHeartRate" sourceName="Apple Watch" unit="count/min" startDate="2026-10-20 08:00:00 +0200" endDate="2026-10-20 08:00:00 +0200" value="92"/>
</HealthData>
`

func TestRenderHealthImports(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	u := New(a)
	u.Attach(syncer.NewManager(config.Default(), st, device.NewMTPScanner(), u.Changed, u.Notify))
	u.window.Resize(fyne.NewSize(640, 460))
	u.tabs.SelectIndex(1)
	capture(t, u.window, "health-tab")

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "export.zip")
	out, _ := os.Create(zipPath)
	zw := zip.NewWriter(out)
	f, _ := zw.Create("apple_health_export/export.xml")
	f.Write([]byte(appleXML))
	zw.Close()
	out.Close()
	export, err := health.ReadAppleExport(zipPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := u.importWindow("Apple Health import")
	u.appleSelection(w, export)
	capture(t, w, "apple-selection")

	csvPath := filepath.Join(dir, "weight.csv")
	os.WriteFile(csvPath, []byte("Date,\"Weight (kg)\",\"Fat mass (kg)\",\"Bone mass (kg)\",\"Muscle mass (kg)\",\"Hydration (kg)\",Comments\n"+
		"\"2026-10-02 07:31:00\",72.0,14.4,3.1,57.6,39.6,\n\"2026-10-01 07:30:00\",72.5,,,,,\n\"oops\",71,,,,,\n"), 0644)
	file, err := health.ReadCSV(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	w = u.importWindow("CSV import")
	u.csvMapping(w, csvPath, file)
	w.Resize(fyne.NewSize(720, 900))
	capture(t, w, "csv-mapping")
}

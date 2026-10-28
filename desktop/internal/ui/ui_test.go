package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"MySportWebSync/internal/config"
	"MySportWebSync/internal/device"
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
	if u.tabs.SelectedIndex() != 2 {
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

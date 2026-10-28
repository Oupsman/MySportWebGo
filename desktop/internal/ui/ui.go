// Package ui is the window and the system tray menu of the application
package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"MySportWebSync/internal/api"
	"MySportWebSync/internal/config"
	"MySportWebSync/internal/syncer"
)

// refreshDelay coalesces the refreshes: a synchronization changes the state at each file
const refreshDelay = 200 * time.Millisecond

type UI struct {
	app     fyne.App
	window  fyne.Window
	manager *syncer.Manager
	tabs    *container.AppTabs

	devices *fyne.Container
	notices *widget.Label
	history *widget.List
	events  []syncer.Event

	refresh chan struct{}
}

// New builds the window; the manager is set by Attach, it needs the callbacks of the UI
func New(a fyne.App) *UI {
	a.SetIcon(icon)
	u := &UI{app: a, window: a.NewWindow("MySportWeb Sync"), refresh: make(chan struct{}, 1)}
	return u
}

// Changed asks for a refresh, from any goroutine
func (u *UI) Changed() {
	select {
	case u.refresh <- struct{}{}:
	default:
	}
}

// Notify shows a notification of the system, from any goroutine
func (u *UI) Notify(text string) {
	u.app.SendNotification(fyne.NewNotification("MySportWeb Sync", text))
}

func (u *UI) Attach(m *syncer.Manager) {
	u.manager = m

	u.devices = container.NewVBox()
	u.notices = widget.NewLabel("")
	u.notices.Wrapping = fyne.TextWrapWord
	u.notices.Importance = widget.WarningImportance
	syncAll := widget.NewButtonWithIcon("Synchronize all", theme.ViewRefreshIcon(), m.SyncAll)
	devicesTab := container.NewBorder(nil, container.NewVBox(u.notices, syncAll), nil, nil,
		container.NewVScroll(u.devices))

	u.history = widget.NewList(
		func() int { return len(u.events) },
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			label := o.(*widget.Label)
			e := u.events[i]
			parts := []string{e.At.Format("Jan 02 15:04:05")}
			for _, part := range []string{e.Device, e.File, e.Text} {
				if part != "" {
					parts = append(parts, part)
				}
			}
			label.SetText(strings.Join(parts, " · "))
			if e.Failed {
				label.Importance = widget.DangerImportance
			} else {
				label.Importance = widget.MediumImportance
			}
			label.Refresh()
		},
	)

	settingsTab := container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), u.settings())
	u.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Devices", theme.ComputerIcon(), devicesTab),
		container.NewTabItemWithIcon("Health", theme.AccountIcon(), u.healthTab()),
		container.NewTabItemWithIcon("History", theme.HistoryIcon(), u.history),
		settingsTab,
	)
	u.window.SetContent(u.tabs)
	u.window.Resize(fyne.NewSize(640, 460))

	if tray, ok := u.app.(desktop.App); ok {
		tray.SetSystemTrayIcon(icon)
		tray.SetSystemTrayMenu(fyne.NewMenu("MySportWeb Sync",
			fyne.NewMenuItem("Open", u.Show),
			fyne.NewMenuItem("Synchronize all", m.SyncAll),
		))
		// the window hides: the application keeps watching the devices from the tray, Quit is in its menu
		u.window.SetCloseIntercept(u.window.Hide)
	}
	if m.Config().Token == "" {
		u.tabs.Select(settingsTab)
	}
	u.render()
}

func (u *UI) Show() {
	u.window.Show()
	u.window.RequestFocus()
}

// Run refreshes the window on the changes until ctx is done, it returns at once
func (u *UI) Run(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-u.refresh:
				fyne.Do(u.render)
				time.Sleep(refreshDelay)
			}
		}
	}()
}

func (u *UI) render() {
	u.events = u.manager.Events()
	u.history.Refresh()

	u.devices.RemoveAll()
	devices := u.manager.Devices()
	if len(devices) == 0 {
		hint := widget.NewLabel("No device. Plug a watch or a bike computer through USB: in mass storage mode it " +
			"must be mounted, in MTP mode it must not be held by another program.")
		hint.Wrapping = fyne.TextWrapWord
		u.devices.Add(hint)
	}
	for _, d := range devices {
		u.devices.Add(u.deviceCard(d))
	}
	u.devices.Refresh()

	u.notices.SetText(strings.Join(u.manager.Notices(), "\n"))
	if u.notices.Text == "" {
		u.notices.Hide()
	} else {
		u.notices.Show()
	}
}

func (u *UI) deviceCard(d syncer.DeviceStatus) fyne.CanvasObject {
	status := widget.NewLabel(d.Last)
	status.Wrapping = fyne.TextWrapWord
	if d.LastErr {
		status.Importance = widget.DangerImportance
	}
	var body fyne.CanvasObject = status
	button := widget.NewButtonWithIcon("Synchronize", theme.ViewRefreshIcon(), func() { u.manager.Sync(d.ID) })
	if d.Syncing {
		button.Disable()
		progress := widget.NewProgressBar()
		progress.Max = float64(max(d.Total, 1))
		progress.SetValue(float64(d.Done))
		progress.TextFormatter = func() string {
			if d.Total == 0 {
				return "listing the activities…"
			}
			return fmt.Sprintf("%d / %d", d.Done, d.Total)
		}
		body = progress
	} else if d.Last == "" {
		status.SetText("not synchronized yet")
	}
	// the wrapped status takes the whole width: beside the button, it would wrap at each word
	return widget.NewCard(d.Name, string(d.Kind), container.NewVBox(body, container.NewHBox(layout.NewSpacer(), button)))
}

func (u *UI) settings() fyne.CanvasObject {
	cfg := u.manager.Config()

	server := widget.NewEntry()
	server.SetPlaceHolder("https://mysportweb.example.com")
	server.SetText(cfg.ServerURL)
	token := widget.NewPasswordEntry()
	token.SetPlaceHolder(api.TokenPrefix + "…")
	token.SetText(cfg.Token)
	folders := widget.NewMultiLineEntry()
	folders.SetText(strings.Join(cfg.Folders, "\n"))
	folders.SetMinRowsVisible(3)
	auto := widget.NewCheck("Synchronize a device when it is plugged", nil)
	auto.SetChecked(cfg.AutoSync)
	maxAge := widget.NewEntry()
	maxAge.SetPlaceHolder("0: all the activities")
	if cfg.MaxAgeDays > 0 {
		maxAge.SetText(strconv.Itoa(cfg.MaxAgeDays))
	}

	read := func() (config.Config, error) {
		next := u.manager.Config()
		next.ServerURL = strings.TrimRight(strings.TrimSpace(server.Text), "/")
		next.Token = strings.TrimSpace(token.Text)
		next.Folders = nil
		for _, line := range strings.Split(folders.Text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				next.Folders = append(next.Folders, line)
			}
		}
		if len(next.Folders) == 0 {
			return next, fmt.Errorf("at least one activity folder is needed")
		}
		next.AutoSync = auto.Checked
		next.MaxAgeDays = 0
		if text := strings.TrimSpace(maxAge.Text); text != "" {
			days, err := strconv.Atoi(text)
			if err != nil || days < 0 {
				return next, fmt.Errorf("the maximum age is a number of days")
			}
			next.MaxAgeDays = days
		}
		if _, err := api.New(next.ServerURL, next.Token); err != nil {
			return next, err
		}
		return next, nil
	}

	test := widget.NewButtonWithIcon("Test the server", theme.SearchIcon(), nil)
	test.OnTapped = func() {
		next, err := read()
		if err != nil {
			dialog.ShowError(err, u.window)
			return
		}
		client, _ := api.New(next.ServerURL, next.Token)
		test.Disable()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			title, text, err := checkServer(ctx, client)
			fyne.Do(func() {
				test.Enable()
				if err != nil {
					dialog.ShowError(err, u.window)
					return
				}
				dialog.ShowInformation(title, text, u.window)
			})
		}()
	}
	save := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		next, err := read()
		if err != nil {
			dialog.ShowError(err, u.window)
			return
		}
		if err := config.Save(next); err != nil {
			dialog.ShowError(err, u.window)
			return
		}
		u.manager.SetConfig(next)
		dialog.ShowInformation("Settings", "Saved.", u.window)
	})
	save.Importance = widget.HighImportance

	form := widget.NewForm(
		widget.NewFormItem("Server URL", server),
		&widget.FormItem{Text: "Device token", Widget: token,
			HintText: "Create it in MySportWeb, with the activities:create permission"},
		&widget.FormItem{Text: "Activity folders", Widget: folders,
			HintText: "One per line, from the root of the device, case ignored"},
		widget.NewFormItem("", auto),
		&widget.FormItem{Text: "Ignore older than", Widget: maxAge, HintText: "days"},
	)
	return container.NewVScroll(container.NewVBox(form, container.NewHBox(test, save)))
}

// checkServer checks the server answers and accepts the token for the uploads
func checkServer(ctx context.Context, client *api.Client) (title, text string, err error) {
	if err := client.Ping(ctx); err != nil {
		return "", "", err
	}
	info, err := client.CheckToken(ctx)
	if errors.Is(err, api.ErrNoTokenRoute) {
		return "Server", "The server answers, but it is too old to check the token: it is checked at the first " +
			"upload, it needs the activities:create permission.", nil
	}
	if err != nil {
		return "", "", err
	}
	if !info.CanUpload() {
		return "", "", fmt.Errorf("the token %q lacks the activities:create permission, it has: %s",
			info.Name, strings.Join(info.Permissions, ", "))
	}
	text = fmt.Sprintf("The server accepts the token %q (%s…).\nIt expires on %s.",
		info.Name, info.Hint, info.ExpiresAt.Local().Format("January 2, 2006"))
	if days := int(time.Until(info.ExpiresAt).Hours() / 24); days < 30 {
		text += fmt.Sprintf("\n\nIt expires in %d days: create a new one in MySportWeb soon.", days)
	}
	if !info.CanImportHealth() {
		text += "\n\nThe health imports need the healthdatas:create permission, this token lacks it."
	}
	return "Token", text, nil
}

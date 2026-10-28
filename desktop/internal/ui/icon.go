package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// iconSVG is the icon of the window and the system tray: two arrows around a watch face. install.sh
// installs the same file for the menu entry.
//
//go:embed icon.svg
var iconSVG []byte

var icon = fyne.NewStaticResource("mysportweb-sync.svg", iconSVG)

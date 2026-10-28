#!/usr/bin/env bash
# Builds and installs MySportWeb Sync on Linux: the binary, its icon and its menu entry, and optionally an
# autostart entry. Run ./install.sh --help for the options.
set -euo pipefail

NAME=mysportweb-sync
APP_ID=io.github.oupsman.mysportwebsync # the id given to Fyne in main.go: its preferences directory
SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PREFIX="$HOME/.local"
AUTOSTART=0
UNINSTALL=0
PURGE=0
INSTALL_DEPS=0

usage() {
	cat <<EOF
Usage: $0 [options]

  --prefix DIR     install under DIR (default: ~/.local; /usr/local for all the users, with sudo)
  --autostart      start the application in the system tray at login
  --install-deps   install the build dependencies with the package manager (sudo)
  --uninstall      remove the installed files, the settings and the synchronization state are kept
  --purge          with --uninstall, also remove the settings (with the token) and the state
  -h, --help       show this help
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix)
		[ $# -ge 2 ] || { usage >&2; exit 2; }
		PREFIX="$2"
		shift
		;;
	--prefix=*) PREFIX="${1#*=}" ;;
	--autostart) AUTOSTART=1 ;;
	--install-deps) INSTALL_DEPS=1 ;;
	--uninstall) UNINSTALL=1 ;;
	--purge) PURGE=1 ;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		usage >&2
		exit 2
		;;
	esac
	shift
done

if [ "$(uname -s)" != Linux ]; then
	echo "this script installs on Linux only, build with 'go build' elsewhere (see README.md)" >&2
	exit 1
fi
if [ "$(id -u)" -eq 0 ]; then
	echo "run it as your user: it asks for sudo when the prefix needs it, the build must not run as root" >&2
	exit 1
fi
if [ "$PURGE" -eq 1 ] && [ "$UNINSTALL" -eq 0 ]; then
	echo "--purge goes with --uninstall" >&2
	exit 2
fi

PREFIX="${PREFIX%/}"
BIN="$PREFIX/bin/$NAME"
ICON="$PREFIX/share/icons/hicolor/scalable/apps/$NAME.svg"
DESKTOP_FILE="$PREFIX/share/applications/$NAME.desktop"
CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
AUTOSTART_FILE="$CONFIG_HOME/autostart/$NAME.desktop"

# as_needed runs a command with sudo when the directory it writes to is not writable by the user
as_needed() {
	local dir="$1"
	shift
	while [ ! -e "$dir" ]; do dir="$(dirname "$dir")"; done
	if [ -w "$dir" ]; then
		"$@"
	else
		sudo "$@"
	fi
}

refresh_caches() {
	command -v update-desktop-database >/dev/null && as_needed "$PREFIX/share/applications" \
		update-desktop-database -q "$PREFIX/share/applications" 2>/dev/null || true
	command -v gtk-update-icon-cache >/dev/null && [ -f "$PREFIX/share/icons/hicolor/index.theme" ] &&
		as_needed "$PREFIX/share/icons/hicolor" gtk-update-icon-cache -q -t "$PREFIX/share/icons/hicolor" 2>/dev/null || true
}

if [ "$UNINSTALL" -eq 1 ]; then
	if pgrep -x "$NAME" >/dev/null; then
		echo "$NAME is running: quit it from its tray menu first" >&2
		exit 1
	fi
	for f in "$BIN" "$ICON" "$DESKTOP_FILE"; do
		[ -e "$f" ] && as_needed "$(dirname "$f")" rm -f "$f" && echo "removed $f"
	done
	[ -e "$AUTOSTART_FILE" ] && rm -f "$AUTOSTART_FILE" && echo "removed $AUTOSTART_FILE"
	refresh_caches
	if [ "$PURGE" -eq 1 ]; then
		for d in "$CONFIG_HOME/$NAME" "$CONFIG_HOME/fyne/$APP_ID"; do
			[ -e "$d" ] && rm -rf "$d" && echo "removed $d"
		done
	else
		echo "kept the settings and the state in $CONFIG_HOME/$NAME (--purge removes them)"
	fi
	exit 0
fi

install_deps() {
	. /etc/os-release
	case " ${ID:-} ${ID_LIKE:-} " in
	*" arch "*)
		sudo pacman -S --needed go pkgconf libmtp mesa libxcursor libxrandr libxinerama libxi libxxf86vm \
			wayland libxkbcommon
		;;
	*" fedora "* | *" rhel "*)
		sudo dnf install -y golang pkgconf-pkg-config libmtp-devel mesa-libGL-devel libXcursor-devel \
			libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel wayland-devel libxkbcommon-devel
		;;
	*" debian "* | *" ubuntu "*)
		# the go of the distribution may be too old: the check below says so
		sudo apt-get install -y golang-go pkg-config libmtp-dev libgl1-mesa-dev xorg-dev libwayland-dev \
			libxkbcommon-dev
		;;
	*" opensuse "* | *" suse "*)
		sudo zypper install -y go pkg-config libmtp-devel Mesa-libGL-devel libXcursor-devel libXrandr-devel \
			libXinerama-devel libXi-devel libXxf86vm-devel wayland-devel libxkbcommon-devel
		;;
	*)
		echo "unknown distribution '${ID:-}': install Go, pkg-config, and the development files of libmtp," \
			"OpenGL, X11 (Xcursor, Xrandr, Xinerama, Xi, Xxf86vm), Wayland and xkbcommon" >&2
		exit 1
		;;
	esac
}

[ "$INSTALL_DEPS" -eq 1 ] && install_deps

# Go 1.21 or later fetches by itself the toolchain go.mod asks for
if ! command -v go >/dev/null; then
	echo "Go is missing: install it (--install-deps, or https://go.dev/dl/)" >&2
	exit 1
fi
GO_MINOR="$(go env GOVERSION | sed -E 's/^go1\.([0-9]+).*/\1/')"
if ! [ "$GO_MINOR" -ge 21 ] 2>/dev/null; then
	echo "$(go env GOVERSION) is too old, Go 1.21 or later is needed (https://go.dev/dl/)" >&2
	exit 1
fi
missing=()
command -v pkg-config >/dev/null || missing+=(pkg-config)
if command -v pkg-config >/dev/null; then
	for lib in libmtp gl x11 xcursor xrandr xinerama xi xxf86vm; do
		pkg-config --exists "$lib" || missing+=("$lib")
	done
fi
if [ ${#missing[@]} -gt 0 ]; then
	echo "missing build dependencies: ${missing[*]}" >&2
	echo "install them with: $0 --install-deps" >&2
	exit 1
fi

echo "building $NAME..."
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT
(cd "$SRC" && CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$BUILD_DIR/$NAME" .)

cat >"$BUILD_DIR/$NAME.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=MySportWeb Sync
GenericName=Activity synchronization
Comment=Upload the activities of the devices plugged through USB to MySportWeb
Exec=$BIN
Icon=$ICON
Terminal=false
Categories=Utility;
Keywords=garmin;fit;mtp;sport;activity;
EOF
command -v desktop-file-validate >/dev/null && desktop-file-validate "$BUILD_DIR/$NAME.desktop"

as_needed "$PREFIX" install -Dm755 "$BUILD_DIR/$NAME" "$BIN"
as_needed "$PREFIX" install -Dm644 "$SRC/internal/ui/icon.svg" "$ICON"
as_needed "$PREFIX" install -Dm644 "$BUILD_DIR/$NAME.desktop" "$DESKTOP_FILE"
refresh_caches
echo "installed $BIN"
echo "installed the menu entry $DESKTOP_FILE"

if [ "$AUTOSTART" -eq 1 ]; then
	mkdir -p "$(dirname "$AUTOSTART_FILE")"
	sed -e "s|^Exec=.*|Exec=$BIN -hidden|" "$BUILD_DIR/$NAME.desktop" >"$AUTOSTART_FILE"
	echo "installed the autostart entry $AUTOSTART_FILE"
fi

# the udev rules of libmtp give the session user access to the MTP devices
rules=0
for pattern in /usr/lib/udev/rules.d/*libmtp* /lib/udev/rules.d/*libmtp* /etc/udev/rules.d/*libmtp*; do
	[ -e "$pattern" ] && rules=1
done
if [ "$rules" -eq 0 ]; then
	echo "warning: the udev rules of libmtp are not installed, the MTP devices may need root" >&2
fi
case ":$PATH:" in
*":$PREFIX/bin:"*) ;;
*) echo "note: $PREFIX/bin is not in PATH, the menu entry works anyway" ;;
esac
if pgrep -x "$NAME" >/dev/null; then
	echo "$NAME is running: quit it from its tray menu and start it again to use the new version"
fi

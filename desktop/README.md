# MySportWeb Sync

A desktop application that uploads to MySportWeb the activities of a watch or a bike computer plugged through USB:

* **USB mass storage**: the device is mounted as a disk (older Garmin watches, Edge, SD cards).
* **MTP**: the device is a media device (recent Garmin watches), read with libmtp, or through GVfs when GVfs
  already holds the device.

It watches the devices, and uploads the new FIT files of their activity folders (`GARMIN/Activity` by default, any
case) when one is plugged. It stays in the system tray when its window is closed.

It is a separate Go module: the backend is built without cgo, this application needs it (Fyne, libmtp).

## Install

On Linux, `install.sh` builds the application and installs it with its menu entry, in `~/.local` by default:

```sh
cd desktop
./install.sh --install-deps   # once: the build dependencies, with the package manager (Arch, Debian/Ubuntu, Fedora, openSUSE)
./install.sh --autostart      # build, install, and start in the system tray at login
./install.sh --prefix /usr/local                    # for all the users, asks for sudo to copy the files
./install.sh --uninstall [--purge]                  # --purge also removes the settings and the state
```

Run it again to update. Debian's Go may be too old: Go 1.21 or later is needed (it fetches the version of
`go.mod` by itself).

## Build

Linux, the development packages of libmtp, OpenGL and X11/Wayland:

```sh
# Arch
sudo pacman -S libmtp mesa libxcursor libxrandr libxinerama libxi libxxf86vm wayland libxkbcommon
# Debian/Ubuntu
sudo apt install libmtp-dev libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev

cd desktop
go build -o mysportweb-sync .
go test ./...
```

macOS: `brew install libmtp pkg-config`, then the same build. Windows builds without MTP (libmtp would need the WinUSB
driver, which replaces the MTP driver of Windows): mass storage devices only. Only Linux has been tested.

## Use

1. Create a device token in MySportWeb with the `activities:create` permission (`POST /api/v1/user/tokens` with a
   session, the token is shown once).
2. Start `mysportweb-sync`, set the server URL (the one opened in the browser) and the token in Settings, save.
   **Test the server** checks the server accepts the token and it holds `activities:create` (`GET /api/v1/token`,
   open to any device token; an older server without this route is only told apart at the first upload).
3. Plug the device. In MTP mode, unlock it if it asks which USB mode to use.

`mysportweb-sync -scan` prints the devices found and their activity files, without uploading: start there when a
device is not listed. `-hidden` starts in the system tray, as the
autostart entry of `install.sh --autostart` does.

## Health imports

The **Health** tab imports health measures into MySportWeb: weight, body fat, muscle, bone, body water, resting heart
rate, VO2max and blood pressure. The token needs `healthdatas:create`.

* **Apple Health**: the `export.zip` of *Export All Health Data* in the Health application of the iPhone. The
  application lists the data MySportWeb stores (weight, body fat, resting heart rate, VO2max, blood pressure) with
  their number and dates, and the others, which aren't imported. Choose the kinds, the sources and a start date.
  The values recorded at the same time (a scale's weight and fat) make one measure.
* **CSV**: any export or spreadsheet. The delimiter, the header, the format of the dates and the columns are
  guessed (English and French names), and can be changed; masses (Withings' "Fat mass (kg)") become percentages
  of the weight; the masses can be in pounds. A preview shows the measures, and the rows left out with their line.

The server completes a measure of the same date instead of duplicating it, and never changes a value already
measured: a file can be imported again.

## How it works

* The devices are scanned every 3 seconds: the mounted volumes (`/proc/self/mountinfo`, `/Volumes`, the drives),
  the MTP devices mounted by GVfs, and the MTP devices of libmtp. An MTP device is opened when plugged to look for its
  activity folders, then closed; it is opened again for each synchronization only.
* A Garmin device is known by the unit id of `GARMIN/GarminDevice.xml`, the same in both modes.
* `~/.config/mysportweb-sync/state.json` records each file (device, path, size, date) and its SHA-256: a known file
  is not read again, a known content is not sent again (the same file from another mode or device).
* The server refuses an activity it already has (409): it is recorded as such, nothing is duplicated, even after
  losing `state.json`.
* A file the server refuses (400) is not sent again by the automatic synchronizations, the **Synchronize** button
  tries it again: the server also answers 400 when drawing the thumbnail fails.
* A refused token (401, 403) stops the synchronization: nothing is recorded, everything is sent once fixed.
* `~/.config/mysportweb-sync/config.json` holds the token in clear, readable by the user only.

## MTP troubleshooting

* *could not open the device: another program may hold it*: GVfs (GNOME, the file managers), KDE's kiod/MTP
  KIO worker or another application claimed the device. With GVfs, the device is read through its mount instead,
  nothing to do. Otherwise close the program, or unmount the device in the file manager; it is tried again
  every 30 seconds.
* *the device shows no storage*: unlock the device, or choose file transfer on it.
* Permission denied: the udev rules of libmtp (`69-libmtp.rules`, installed with libmtp) give the session user
  access to the known MTP devices.

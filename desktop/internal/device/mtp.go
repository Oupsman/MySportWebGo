//go:build cgo && !windows

package device

/*
#cgo pkg-config: libmtp
#include <stdlib.h>
#include <libmtp.h>

static LIBMTP_raw_device_t *raw_at(LIBMTP_raw_device_t *raws, int i) { return &raws[i]; }
*/
import "C"

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// libmtp is not thread safe: every call holds mtpLock
var (
	mtpLock sync.Mutex
	mtpInit sync.Once
)

// rawKey identifies a plugged MTP device, until it is unplugged
type rawKey struct {
	bus       uint32
	devnum    uint8
	vendorID  uint16
	productID uint16
}

type probe struct {
	device   *mtpDevice // nil when the device has no activity folder, or could not be opened
	notice   string
	probedAt time.Time
}

// MTPScanner finds the MTP devices with libmtp. It opens each new device once, to read its name and
// look for the activity folders, and closes it: the file manager can open it meanwhile.
type MTPScanner struct {
	mu      sync.Mutex
	probes  map[rawKey]*probe
	folders []string // the folders of the probes

	fingerprint string   // the USB devices at the last detection
	keys        []rawKey // the MTP devices found then
}

func NewMTPScanner() *MTPScanner {
	return &MTPScanner{probes: map[rawKey]*probe{}}
}

func (s *MTPScanner) Scan(ctx context.Context, folders []string) ([]Device, error) {
	mtpInit.Do(func() {
		C.LIBMTP_Init()
		C.LIBMTP_Set_Debug(0)
	})
	s.mu.Lock()
	defer s.mu.Unlock()

	// libmtp probes the unknown USB devices to find the MTP ones: only when the USB devices change
	fingerprint := usbFingerprint()
	retry := false
	for _, p := range s.probes {
		retry = retry || (p.notice != "" && time.Since(p.probedAt) >= probeRetry)
	}
	keys := s.keys
	if fingerprint == "" || fingerprint != s.fingerprint || retry {
		var err error
		if keys, err = detect(); err != nil {
			return nil, err
		}
		s.keys, s.fingerprint = keys, fingerprint
	}

	if !slices.Equal(folders, s.folders) {
		for key, p := range s.probes {
			if p.device != nil {
				p.device.Close()
			}
			delete(s.probes, key)
		}
		s.folders = slices.Clone(folders)
	}
	present := map[rawKey]bool{}
	for _, key := range keys {
		present[key] = true
		p := s.probes[key]
		if p != nil && (p.device != nil || p.notice == "" || time.Since(p.probedAt) < probeRetry) {
			continue
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		device, notice := probeDevice(key, folders)
		s.probes[key] = &probe{device: device, notice: notice, probedAt: time.Now()}
	}
	var devices []Device
	for key, p := range s.probes {
		if !present[key] {
			if p.device != nil {
				p.device.Close()
			}
			delete(s.probes, key)
			continue
		}
		if p.device != nil {
			devices = append(devices, p.device)
		}
	}
	return devices, nil
}

// Notices returns why plugged MTP devices are not listed, e.g. a device held by another program
func (s *MTPScanner) Notices() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var notices []string
	for _, p := range s.probes {
		if p.notice != "" {
			notices = append(notices, p.notice)
		}
	}
	sort.Strings(notices)
	return notices
}

// usbFingerprint changes when a USB device is plugged or unplugged, "" when unknown (not Linux)
func usbFingerprint() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	entries, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries {
		// the device number changes at each plug, even on the same port
		devnum, _ := os.ReadFile(filepath.Join("/sys/bus/usb/devices", entry.Name(), "devnum"))
		b.WriteString(entry.Name() + "=" + strings.TrimSpace(string(devnum)) + ";")
	}
	return b.String()
}

// detect returns the plugged MTP devices, without opening them
func detect() ([]rawKey, error) {
	mtpLock.Lock()
	defer mtpLock.Unlock()
	var raws *C.LIBMTP_raw_device_t
	var count C.int
	switch status := C.LIBMTP_Detect_Raw_Devices(&raws, &count); status {
	case C.LIBMTP_ERROR_NONE:
	case C.LIBMTP_ERROR_NO_DEVICE_ATTACHED:
		return nil, nil
	default:
		return nil, fmt.Errorf("libmtp: detecting the devices failed (error %d)", int(status))
	}
	defer C.free(unsafe.Pointer(raws))
	keys := make([]rawKey, 0, int(count))
	for i := 0; i < int(count); i++ {
		keys = append(keys, keyOf(C.raw_at(raws, C.int(i))))
	}
	return keys, nil
}

func keyOf(raw *C.LIBMTP_raw_device_t) rawKey {
	return rawKey{
		bus:       uint32(raw.bus_location),
		devnum:    uint8(raw.devnum),
		vendorID:  uint16(raw.device_entry.vendor_id),
		productID: uint16(raw.device_entry.product_id),
	}
}

// open opens a plugged device, mtpLock must be held
func open(key rawKey) (*C.LIBMTP_mtpdevice_t, error) {
	var raws *C.LIBMTP_raw_device_t
	var count C.int
	if C.LIBMTP_Detect_Raw_Devices(&raws, &count) != C.LIBMTP_ERROR_NONE {
		return nil, errors.New("the device is unplugged")
	}
	defer C.free(unsafe.Pointer(raws))
	for i := 0; i < int(count); i++ {
		raw := C.raw_at(raws, C.int(i))
		if keyOf(raw) != key {
			continue
		}
		dev := C.LIBMTP_Open_Raw_Device_Uncached(raw)
		if dev == nil {
			return nil, errors.New("could not open the device: another program (GVfs, KDE Connect, a file manager) may hold it")
		}
		return dev, nil
	}
	return nil, errors.New("the device is unplugged")
}

// probeDevice returns the device when it has an activity folder, or why it is not listed
func probeDevice(key rawKey, folders []string) (*mtpDevice, string) {
	label := fmt.Sprintf("MTP device %04x:%04x", key.vendorID, key.productID)
	d := &mtpDevice{key: key, folders: folders}
	err := d.with(func(dev *C.LIBMTP_mtpdevice_t) error {
		serial := takeString(C.LIBMTP_Get_Serialnumber(dev))
		d.name = takeString(C.LIBMTP_Get_Friendlyname(dev))
		if d.name == "" {
			d.name = strings.TrimSpace(takeString(C.LIBMTP_Get_Manufacturername(dev)) + " " + takeString(C.LIBMTP_Get_Modelname(dev)))
		}
		if d.name != "" {
			label = d.name
		} else {
			d.name = label
		}
		d.id = fmt.Sprintf("mtp:%04x_%04x_%s", key.vendorID, key.productID, serial)

		dirs, err := d.activityDirs(dev)
		if err != nil {
			return err
		}
		if len(dirs) == 0 {
			return nil
		}
		d.hasFolders = true
		for _, storage := range storages(dev) {
			id, ok := resolveMTP(dev, storage.id, splitFolder(garminDeviceXML), false)
			if !ok {
				continue
			}
			var buf bytes.Buffer
			if download(dev, id, &buf) == nil {
				if info, ok := parseGarminDevice(&buf); ok {
					d.id = "garmin:" + info.ID
					if info.Model != "" {
						d.name = info.Model
					}
				}
			}
			break
		}
		return nil
	})
	d.Close()
	if err != nil {
		return nil, label + ": " + err.Error()
	}
	if !d.hasFolders {
		return nil, "" // a phone or a player: not a sport device
	}
	return d, ""
}

// mtpDevice opens its device at the first use, until Close
type mtpDevice struct {
	key        rawKey
	id, name   string
	folders    []string
	hasFolders bool

	mu  sync.Mutex
	dev *C.LIBMTP_mtpdevice_t
}

func (d *mtpDevice) ID() string   { return d.id }
func (d *mtpDevice) Name() string { return d.name }
func (d *mtpDevice) Kind() Kind   { return MTP }

// with runs fn with the open device, opening it when needed
func (d *mtpDevice) with(fn func(dev *C.LIBMTP_mtpdevice_t) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	mtpLock.Lock()
	defer mtpLock.Unlock()
	if d.dev == nil {
		dev, err := open(d.key)
		if err != nil {
			return err
		}
		d.dev = dev
		C.LIBMTP_Get_Storage(dev, C.LIBMTP_STORAGE_SORTBY_NOTSORTED)
	}
	err := fn(d.dev)
	if err != nil {
		if stack := errorStack(d.dev); stack != "" {
			err = fmt.Errorf("%w (%s)", err, stack)
		}
	}
	C.LIBMTP_Clear_Errorstack(d.dev)
	return err
}

// Close releases the device, the next use opens it again
func (d *mtpDevice) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dev != nil {
		mtpLock.Lock()
		C.LIBMTP_Release_Device(d.dev)
		mtpLock.Unlock()
		d.dev = nil
	}
}

type mtpStorage struct {
	id   uint32
	name string
}

func storages(dev *C.LIBMTP_mtpdevice_t) []mtpStorage {
	var list []mtpStorage
	for s := dev.storage; s != nil; s = s.next {
		name := C.GoString(s.StorageDescription)
		if name == "" {
			name = fmt.Sprintf("storage-%08x", uint32(s.id))
		}
		list = append(list, mtpStorage{id: uint32(s.id), name: name})
	}
	return list
}

type mtpDir struct {
	storage uint32
	id      uint32
	path    string // storage name + folder, e.g. "Internal Storage/GARMIN/Activity"
}

func (d *mtpDevice) activityDirs(dev *C.LIBMTP_mtpdevice_t) ([]mtpDir, error) {
	list := storages(dev)
	if len(list) == 0 {
		return nil, errors.New("the device shows no storage: unlock it, or choose file transfer on it")
	}
	var dirs []mtpDir
	for _, storage := range list {
		for _, folder := range d.folders {
			parts := splitFolder(folder)
			if id, ok := resolveMTP(dev, storage.id, parts, true); ok {
				dirs = append(dirs, mtpDir{storage.id, id, path.Join(append([]string{storage.name}, parts...)...)})
			}
		}
	}
	return dirs, nil
}

type mtpEntry struct {
	id      uint32
	name    string
	size    int64
	modTime time.Time
	folder  bool
}

// children lists a folder of a storage, C.LIBMTP_FILES_AND_FOLDERS_ROOT for its root
func children(dev *C.LIBMTP_mtpdevice_t, storage, parent uint32) []mtpEntry {
	var entries []mtpEntry
	file := C.LIBMTP_Get_Files_And_Folders(dev, C.uint32_t(storage), C.uint32_t(parent))
	for file != nil {
		entries = append(entries, mtpEntry{
			id:      uint32(file.item_id),
			name:    C.GoString(file.filename),
			size:    int64(file.filesize),
			modTime: time.Unix(int64(file.modificationdate), 0),
			folder:  file.filetype == C.LIBMTP_FILETYPE_FOLDER,
		})
		next := file.next
		C.LIBMTP_destroy_file_t(file)
		file = next
	}
	return entries
}

// resolveMTP returns the object of the path components in a storage, each matched without case.
// The last one must be a folder when folder is true, a file otherwise.
func resolveMTP(dev *C.LIBMTP_mtpdevice_t, storage uint32, parts []string, folder bool) (uint32, bool) {
	parent := uint32(C.LIBMTP_FILES_AND_FOLDERS_ROOT)
	for i, part := range parts {
		wantFolder := folder || i < len(parts)-1
		found, ok := uint32(0), false
		for _, entry := range children(dev, storage, parent) {
			if entry.folder == wantFolder && strings.EqualFold(entry.name, part) {
				found, ok = entry.id, true
				if entry.name == part {
					break
				}
			}
		}
		if !ok {
			return 0, false
		}
		parent = found
	}
	return parent, len(parts) > 0
}

func (d *mtpDevice) Files(ctx context.Context) ([]File, error) {
	var files []File
	err := d.with(func(dev *C.LIBMTP_mtpdevice_t) error {
		dirs, err := d.activityDirs(dev)
		if err != nil {
			return err
		}
		for _, dir := range dirs {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			for _, entry := range children(dev, dir.storage, dir.id) {
				if !entry.folder && isFIT(entry.name) {
					files = append(files, File{Path: path.Join(dir.path, entry.name), Size: entry.size, ModTime: entry.modTime, id: entry.id})
				}
			}
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.Before(files[j].ModTime) })
	return files, err
}

func (d *mtpDevice) Copy(ctx context.Context, f File, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.with(func(dev *C.LIBMTP_mtpdevice_t) error {
		return download(dev, f.id, w)
	})
}

// download copies an object to w, through a temporary file: libmtp writes to a file descriptor
func download(dev *C.LIBMTP_mtpdevice_t, id uint32, w io.Writer) error {
	tmp, err := os.CreateTemp("", "mswsync-*.fit")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if C.LIBMTP_Get_File_To_File_Descriptor(dev, C.uint32_t(id), C.int(tmp.Fd()), nil, nil) != 0 {
		return errors.New("downloading the file failed")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = io.Copy(w, tmp)
	return err
}

func errorStack(dev *C.LIBMTP_mtpdevice_t) string {
	var texts []string
	for e := C.LIBMTP_Get_Errorstack(dev); e != nil; e = e.next {
		if text := strings.TrimSpace(C.GoString(e.error_text)); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "; ")
}

// takeString returns a string allocated by libmtp, and frees it
func takeString(s *C.char) string {
	if s == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(s))
	return C.GoString(s)
}

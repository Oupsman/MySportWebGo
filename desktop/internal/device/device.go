// Package device finds the sport devices plugged in, USB mass storage or MTP, and reads their activity files
package device

import (
	"context"
	"io"
	"path"
	"strings"
	"time"
)

type Kind string

const (
	MassStorage Kind = "USB storage"
	MTP         Kind = "MTP"
)

// File is an activity file of a device, Path is relative to the device and uses slashes
type File struct {
	Path    string
	Size    int64
	ModTime time.Time
	id      uint32 // the MTP object id
}

func (f File) Name() string {
	return path.Base(f.Path)
}

// Device is a plugged device holding at least one activity folder
type Device interface {
	// ID is stable across plugs: the serial number when the device gives it
	ID() string
	Name() string
	Kind() Kind
	// Files lists the FIT files of the activity folders
	Files(ctx context.Context) ([]File, error)
	// Copy writes the content of a file of the device to w
	Copy(ctx context.Context, f File, w io.Writer) error
	// Close releases the device after a synchronization, a later use opens it again
	Close()
}

// Scanner returns the devices currently plugged in
type Scanner interface {
	Scan(ctx context.Context, folders []string) ([]Device, error)
}

// probeRetry is the delay before looking again at a device that could not be opened (e.g. an MTP
// device held by GVfs, or still locked), or had no activity folder
const probeRetry = 30 * time.Second

// DefaultFolders are the activity folders searched on a device, relative to the root of a storage and
// matched without case: Garmin devices write FIT files in GARMIN/Activity, in mass storage and MTP mode
var DefaultFolders = []string{"GARMIN/Activity"}

func isFIT(name string) bool {
	return strings.EqualFold(path.Ext(name), ".fit")
}

// splitFolder returns the components of a configured folder, without empty ones
func splitFolder(folder string) []string {
	var parts []string
	for _, part := range strings.Split(strings.ReplaceAll(folder, "\\", "/"), "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

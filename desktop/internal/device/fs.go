package device

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// root is a mounted file system that may be a device: a USB mass storage volume, or an MTP device
// mounted by GVfs, whose storages are its subdirectories
type root struct {
	Path     string
	ID       string // stable id of the volume when known, the path otherwise
	Label    string
	Kind     Kind
	Storages bool // the activity folders are searched in each subdirectory
}

// FSScanner finds the devices among the mounted file systems. It reads a volume again only once
// unmounted, or after probeRetry when it had no activity folder: through GVfs, each reading of a
// folder is a request to the MTP device.
type FSScanner struct {
	mu      sync.Mutex
	folders []string
	probes  map[root]*fsProbe
}

type fsProbe struct {
	device   *fsDevice // nil without activity folder
	probedAt time.Time
}

func NewFSScanner() *FSScanner {
	return &FSScanner{probes: map[root]*fsProbe{}}
}

func (s *FSScanner) Scan(ctx context.Context, folders []string) ([]Device, error) {
	roots, err := mountRoots()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Equal(folders, s.folders) {
		clear(s.probes)
		s.folders = slices.Clone(folders)
	}
	mounted := map[root]bool{}
	var devices []Device
	for _, r := range roots {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		mounted[r] = true
		p := s.probes[r]
		if p == nil || (p.device == nil && time.Since(p.probedAt) >= probeRetry) {
			p = &fsProbe{device: openFS(r, folders), probedAt: time.Now()}
			s.probes[r] = p
		}
		if p.device != nil {
			devices = append(devices, p.device)
		}
	}
	for r := range s.probes {
		if !mounted[r] {
			delete(s.probes, r)
		}
	}
	return devices, nil
}

// fsDevice reads the files of a mounted device. The paths of its files are relative to its root.
type fsDevice struct {
	id, name string
	kind     Kind
	root     string
	dirs     []string // the activity folders found, relative to root
}

// openFS returns the device of a root holding at least one activity folder, nil otherwise
func openFS(r root, folders []string) *fsDevice {
	storages := []string{""}
	if r.Storages {
		storages = nil
		entries, _ := os.ReadDir(r.Path)
		for _, entry := range entries {
			if entry.IsDir() {
				storages = append(storages, entry.Name())
			}
		}
	}
	d := &fsDevice{id: r.ID, name: r.Label, kind: r.Kind, root: r.Path}
	for _, storage := range storages {
		base := filepath.Join(r.Path, storage)
		for _, folder := range folders {
			if dir, ok := resolveFold(base, splitFolder(folder)); ok {
				rel, _ := filepath.Rel(r.Path, dir)
				d.dirs = append(d.dirs, filepath.ToSlash(rel))
			}
		}
		if dir, ok := resolveFold(base, splitFolder(garminDeviceXML)); ok {
			if f, err := os.Open(dir); err == nil {
				if info, ok := parseGarminDevice(f); ok {
					d.id = "garmin:" + info.ID
					if info.Model != "" {
						d.name = info.Model
					}
				}
				f.Close()
			}
		}
	}
	if len(d.dirs) == 0 {
		return nil
	}
	return d
}

// resolveFold returns the path of the components under base, each matched without case
func resolveFold(base string, parts []string) (string, bool) {
	current := base
	for _, part := range parts {
		entries, err := os.ReadDir(current)
		if err != nil {
			return "", false
		}
		found := ""
		for _, entry := range entries {
			if entry.Name() == part {
				found = part
				break
			}
			if found == "" && strings.EqualFold(entry.Name(), part) {
				found = entry.Name()
			}
		}
		if found == "" {
			return "", false
		}
		current = filepath.Join(current, found)
	}
	return current, true
}

func (d *fsDevice) ID() string   { return d.id }
func (d *fsDevice) Name() string { return d.name }
func (d *fsDevice) Kind() Kind   { return d.kind }
func (d *fsDevice) Close()       {}

func (d *fsDevice) Files(ctx context.Context) ([]File, error) {
	var files []File
	for _, dir := range d.dirs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		entries, err := os.ReadDir(filepath.Join(d.root, filepath.FromSlash(dir)))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !isFIT(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue // removed meanwhile
			}
			files = append(files, File{Path: path.Join(dir, entry.Name()), Size: info.Size(), ModTime: info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime.Before(files[j].ModTime) })
	return files, nil
}

func (d *fsDevice) Copy(ctx context.Context, f File, w io.Writer) error {
	file, err := os.Open(filepath.Join(d.root, filepath.FromSlash(f.Path)))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(w, file)
	return err
}

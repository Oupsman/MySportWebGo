package status

import (
	"path/filepath"
	"syscall"
)

// diskOf returns the space of the filesystem of path, or of its nearest existing parent (the media directory
// is created at the first upload); nil when none can be read
func diskOf(path string) *Disk {
	dir, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	for {
		var st syscall.Statfs_t
		if err := syscall.Statfs(dir, &st); err == nil {
			return &Disk{Path: dir, Free: st.Bavail * uint64(st.Bsize), Total: st.Blocks * uint64(st.Bsize)}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

package status

import (
	"path/filepath"
	"testing"
)

func TestDiskOf(t *testing.T) {
	dir := t.TempDir()
	d := diskOf(filepath.Join(dir, "MEDIA", "not", "created"))
	if d == nil || d.Path != dir || d.Total == 0 || d.Free > d.Total {
		t.Errorf("disk %+v", d)
	}
}

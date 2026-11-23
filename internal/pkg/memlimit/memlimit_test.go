package memlimit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainerLimit(t *testing.T) {
	write := func(t *testing.T, root, name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name, file, content string
		limit               int64
		ok                  bool
	}{
		{"v2 limited", "memory.max", "1073741824\n", 1 << 30, true},
		{"v2 unlimited", "memory.max", "max\n", 0, false},
		{"v1 limited", "memory/memory.limit_in_bytes", "536870912\n", 512 << 20, true},
		{"v1 unlimited", "memory/memory.limit_in_bytes", "9223372036854771712\n", 0, false},
		{"garbage", "memory.max", "lots\n", 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, c.file, c.content)
			limit, ok := ContainerLimit(root)
			if ok != c.ok || (ok && limit != c.limit) {
				t.Errorf("%d %v, want %d %v", limit, ok, c.limit, c.ok)
			}
		})
	}
	if _, ok := ContainerLimit(t.TempDir()); ok {
		t.Error("a limit outside of a cgroup")
	}
}

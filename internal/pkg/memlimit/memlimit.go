// Package memlimit reads the memory limit of the container the backend runs in (mem_limit of
// docker-compose.yaml), to keep the backend under it: the garbage collector of Go works harder before the
// limit (GOMEMLIMIT), the heavy work runs fewer at a time.
package memlimit

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

// SoftPercent is the share of the limit of the container given to Go as its soft limit (GOMEMLIMIT): the
// rest is for what the garbage collector doesn't see (the stacks, the memory not returned yet)
const SoftPercent = 85

// unlimitedV1 is above what cgroup v1 reports without limit (a page-rounded max int64)
const unlimitedV1 = 1 << 62

// ContainerLimit returns the memory limit of the cgroup under root ("/sys/fs/cgroup"), in bytes; false
// without limit, or outside of a container
func ContainerLimit(root string) (int64, bool) {
	// cgroup v2: "max" without limit
	if data, err := os.ReadFile(filepath.Join(root, "memory.max")); err == nil {
		text := strings.TrimSpace(string(data))
		if text == "max" {
			return 0, false
		}
		limit, err := strconv.ParseInt(text, 10, 64)
		return limit, err == nil && limit > 0
	}
	// cgroup v1: a huge number without limit
	if data, err := os.ReadFile(filepath.Join(root, "memory", "memory.limit_in_bytes")); err == nil {
		limit, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		return limit, err == nil && limit > 0 && limit < unlimitedV1
	}
	return 0, false
}

// Apply sets the soft memory limit of Go to SoftPercent of the limit of the container, unless GOMEMLIMIT
// sets it. It returns the limit of the container, 0 without, and the soft limit it set, 0 when it set none.
func Apply() (limit, soft int64) {
	limit, ok := ContainerLimit("/sys/fs/cgroup")
	if !ok {
		return 0, 0
	}
	if os.Getenv("GOMEMLIMIT") != "" {
		return limit, 0
	}
	soft = limit / 100 * SoftPercent
	debug.SetMemoryLimit(soft)
	return limit, soft
}

//go:build !linux

package status

// diskOf: only on Linux, where the backend runs
func diskOf(string) *Disk { return nil }

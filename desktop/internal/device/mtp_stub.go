//go:build !cgo || windows

package device

import "context"

// MTPScanner finds nothing without libmtp: built without cgo, or on Windows (libmtp needs the WinUSB
// driver there, which replaces the MTP driver of Windows)
type MTPScanner struct{}

func NewMTPScanner() *MTPScanner { return &MTPScanner{} }

func (*MTPScanner) Scan(context.Context, []string) ([]Device, error) { return nil, nil }

func (*MTPScanner) Notices() []string { return nil }

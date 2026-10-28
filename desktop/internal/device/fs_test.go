package device

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const garminXML = `<?xml version="1.0" encoding="UTF-8"?>
<Device xmlns="http://www.garmin.com/xmlschemas/GarminDevice/v2">
  <Model><PartNumber>006-B3990-00</PartNumber><Description>Forerunner 255</Description></Model>
  <Id>3456789012</Id>
</Device>`

func write(t *testing.T, p, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if !mod.IsZero() {
		os.Chtimes(p, mod, mod)
	}
}

func TestOpenFSGarmin(t *testing.T) {
	dir := t.TempDir()
	older := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)
	// the case differs from the configured folder, as on some models
	write(t, filepath.Join(dir, "Garmin", "ACTIVITY", "B.FIT"), "bb", newer)
	write(t, filepath.Join(dir, "Garmin", "ACTIVITY", "a.fit"), "a", older)
	write(t, filepath.Join(dir, "Garmin", "ACTIVITY", "notes.txt"), "x", older)
	write(t, filepath.Join(dir, "Garmin", "GarminDevice.xml"), garminXML, time.Time{})

	d := openFS(root{Path: dir, ID: "volume:1234", Label: "GARMIN", Kind: MassStorage}, DefaultFolders)
	if d == nil {
		t.Fatal("the device is not found")
	}
	if d.ID() != "garmin:3456789012" || d.Name() != "Forerunner 255" {
		t.Errorf("id %q, name %q: want the ones of GarminDevice.xml", d.ID(), d.Name())
	}
	files, err := d.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != "Garmin/ACTIVITY/a.fit" || files[1].Path != "Garmin/ACTIVITY/B.FIT" {
		t.Fatalf("files %+v: want the FIT files, oldest first", files)
	}
	var buf bytes.Buffer
	if err := d.Copy(context.Background(), files[1], &buf); err != nil || buf.String() != "bb" {
		t.Errorf("copy: %q, %v", buf.String(), err)
	}
}

func TestOpenFSWithoutActivityFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "DCIM", "photo.jpg"), "x", time.Time{})
	if d := openFS(root{Path: dir, ID: "volume:1", Kind: MassStorage}, DefaultFolders); d != nil {
		t.Errorf("a USB stick is listed: %+v", d)
	}
}

func TestOpenFSStorages(t *testing.T) {
	// an MTP device mounted by GVfs: the activity folder is in a storage
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Internal Storage", "GARMIN", "Activity", "run.fit"), "r", time.Time{})
	d := openFS(root{Path: dir, ID: "mtp:091e_4c29_0000cafe", Label: "091e_4c29_0000cafe", Kind: MTP, Storages: true},
		[]string{"garmin/activity"})
	if d == nil {
		t.Fatal("the device is not found")
	}
	files, err := d.Files(context.Background())
	if err != nil || len(files) != 1 || files[0].Path != "Internal Storage/GARMIN/Activity/run.fit" {
		t.Fatalf("files %+v, %v", files, err)
	}
	if d.ID() != "mtp:091e_4c29_0000cafe" {
		t.Errorf("id %q: without GarminDevice.xml, the id of the root is kept", d.ID())
	}
}

func TestResolveFoldPrefersExactCase(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "activity"), 0755)
	os.MkdirAll(filepath.Join(dir, "Activity"), 0755)
	if p, ok := resolveFold(dir, []string{"Activity"}); !ok || filepath.Base(p) != "Activity" {
		t.Errorf("resolved %q", p)
	}
}

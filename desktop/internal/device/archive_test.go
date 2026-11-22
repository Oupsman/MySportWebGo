package device

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func gzipped(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipped(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	at := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
	for name, content := range files {
		f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: at})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// stravaCSV has the columns of a Strava export: some repeated, a description over two lines
const stravaCSV = "\ufeffActivity ID,Activity Date,Activity Name,Activity Type,Activity Description,Elapsed Time," +
	"Distance,Filename,Elapsed Time,Distance\n" +
	"1,\"May 1, 2024\",Col du Galibier,Ride,\"long\nday\",3600,50,activities/1.fit.gz,3600,50000\n" +
	"2,\"May 2, 2024\",Recovery jog,Run,,1800,5,activities/2.fit,1800,5000\n" +
	"3,\"May 3, 2024\",Phone walk,Walk,,600,1,activities/3.gpx,600,1000\n" +
	"4,\"May 4, 2024\",,Ride,,600,1,activities/4.fit.gz,600,1000\n"

func writeArchive(t *testing.T) string {
	t.Helper()
	nested := zipped(t, map[string][]byte{
		"a.fit":     []byte("nested a"),
		"sub/b.FIT": []byte("nested b"),
		"c.tcx.gz":  gzipped(t, []byte("tcx")),
	})
	archive := zipped(t, map[string][]byte{
		"activities.csv":                 []byte(stravaCSV),
		"activities/1.fit.gz":            gzipped(t, []byte("ride one")),
		"activities/2.fit":               []byte("run two"),
		"activities/3.gpx":               []byte("<gpx/>"),
		"activities/4.fit.gz":            gzipped(t, []byte("ride four")),
		"activities/broken.fit.gz":       []byte("not gzip"),
		"activities/huge.fit.gz":         gzipped(t, make([]byte, MaxFITSize+1)),
		"export/UploadedFiles_Part1.zip": nested,
		"export/broken.zip":              []byte("not a zip"),
		"README.txt":                     []byte("hello"),
	})
	path := filepath.Join(t.TempDir(), "export_123.zip")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArchive(t *testing.T) {
	a, err := OpenArchive(writeArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Kind() != Archive || a.Name() != "export_123.zip" {
		t.Errorf("kind %s, name %s", a.Kind(), a.Name())
	}
	ctx := context.Background()
	files, err := a.Files(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	slices.Sort(paths)
	want := []string{
		"activities/1.fit.gz", "activities/2.fit", "activities/3.gpx", "activities/4.fit.gz",
		"activities/broken.fit.gz", "activities/huge.fit.gz",
		"export/UploadedFiles_Part1.zip!/a.fit", "export/UploadedFiles_Part1.zip!/c.tcx.gz",
		"export/UploadedFiles_Part1.zip!/sub/b.FIT",
	}
	if !slices.Equal(paths, want) {
		t.Errorf("files\n%v\nwant\n%v", paths, want)
	}
	// the broken zip
	if a.Skipped() != 1 {
		t.Errorf("%d skipped, want 1", a.Skipped())
	}

	byPath := map[string]File{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	for p, want := range map[string]string{
		"activities/1.fit.gz":                       "ride one",
		"activities/2.fit":                          "run two",
		"export/UploadedFiles_Part1.zip!/a.fit":     "nested a",
		"export/UploadedFiles_Part1.zip!/sub/b.FIT": "nested b",
	} {
		var buf bytes.Buffer
		if err := a.Copy(ctx, byPath[p], &buf); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if buf.String() != want {
			t.Errorf("%s: %q, want %q", p, buf.String(), want)
		}
	}
	// the GPX and TCX files are sent gzipped: compressed here, as they are when already gzipped
	for p, want := range map[string]string{
		"activities/3.gpx":                         "<gpx/>",
		"export/UploadedFiles_Part1.zip!/c.tcx.gz": "tcx",
	} {
		var buf bytes.Buffer
		if err := a.Copy(ctx, byPath[p], &buf); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		r, err := gzip.NewReader(&buf)
		if err != nil {
			t.Fatalf("%s isn't sent gzipped: %v", p, err)
		}
		content, _ := io.ReadAll(r)
		if string(content) != want {
			t.Errorf("%s: %q, want %q", p, content, want)
		}
	}
	if got := byPath["activities/3.gpx"].UploadName(); got != "3.gpx.gz" {
		t.Errorf("GPX upload name %q", got)
	}
	if got := byPath["export/UploadedFiles_Part1.zip!/c.tcx.gz"].UploadName(); got != "c.tcx.gz" {
		t.Errorf("TCX upload name %q", got)
	}
	for p, want := range map[string]string{
		"activities/1.fit.gz": "cycling", "activities/2.fit": "running", "activities/3.gpx": "walking",
		"export/UploadedFiles_Part1.zip!/a.fit": "",
	} {
		if got := a.Sport(byPath[p]); got != want {
			t.Errorf("sport of %s: %q, want %q", p, got, want)
		}
	}
	for _, p := range []string{"activities/broken.fit.gz", "activities/huge.fit.gz"} {
		var unreadable *UnreadableError
		if err := a.Copy(ctx, byPath[p], &bytes.Buffer{}); !errors.As(err, &unreadable) {
			t.Errorf("%s: %v, want an UnreadableError", p, err)
		}
	}

	for p, want := range map[string]string{
		"activities/1.fit.gz":                   "Col du Galibier",
		"activities/2.fit":                      "Recovery jog",
		"activities/3.gpx":                      "Phone walk",
		"activities/4.fit.gz":                   "", // no name in the export
		"export/UploadedFiles_Part1.zip!/a.fit": "",
	} {
		if got := a.Title(byPath[p]); got != want {
			t.Errorf("title of %s: %q, want %q", p, got, want)
		}
	}
	if got := byPath["activities/1.fit.gz"].UploadName(); got != "1.fit" {
		t.Errorf("upload name %q", got)
	}

	// the temporary file of the zip inside is removed
	a.Close()
	leftovers, _ := filepath.Glob(filepath.Join(os.TempDir(), "mysportweb-archive-*.zip"))
	for _, l := range leftovers {
		if info, err := os.Stat(l); err == nil && time.Since(info.ModTime()) < time.Minute {
			t.Errorf("temporary file left: %s", l)
		}
	}
}

func TestArchiveNotZip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(path, []byte("nope"), 0o600)
	if _, err := OpenArchive(path); err == nil {
		t.Error("a file that isn't a zip opened")
	}
}

func TestArchiveWithoutTitles(t *testing.T) {
	// a CSV without the columns of Strava: no titles, the activities are still imported
	path := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(path, zipped(t, map[string][]byte{
		"activities.csv": []byte("id,name\n1,x\n"),
		"1.fit":          []byte("one"),
	}), 0o600)
	a, err := OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	files, err := a.Files(context.Background())
	if err != nil || len(files) != 1 || a.Title(files[0]) != "" {
		t.Errorf("files %v, %v", files, err)
	}
}

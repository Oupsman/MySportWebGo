package device

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Archive is the export of an account elsewhere (Strava, Garmin Connect…): a zip holding FIT files,
// gzipped or not, at any depth, or inside the zips it holds (one level). It is read where it is: a FIT
// file is decompressed when it is sent, a zip inside to a temporary file while its files are read.
const Archive Kind = "archive"

const (
	// MaxFITSize is the largest FIT file read from an archive, decompressed: the server refuses larger
	// uploads (25 MB), and a decompression bomb stops there
	MaxFITSize = 25 << 20
	// maxNestedSize is the largest zip inside the archive, decompressed to a temporary file
	maxNestedSize = 8 << 30
	// nestedSeparator joins the path of a zip inside the archive and the path of a file in it
	nestedSeparator = "!/"
)

// UnreadableError is a file of an archive that can't be read (corrupt, too large): the others still are
type UnreadableError struct {
	Path string
	Err  error
}

func (e *UnreadableError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *UnreadableError) Unwrap() error { return e.Err }

// Titled is a device knowing the titles of its activities, from elsewhere: a Strava export
type Titled interface {
	Title(f File) string
}

// Skipping is a device holding activity files the server can't read (GPX, TCX), not sent
type Skipping interface {
	Skipped() int
}

// ZipArchive is an Archive device
type ZipArchive struct {
	path string
	id   string

	mu      sync.Mutex
	zip     *zip.ReadCloser
	entries map[string]archiveEntry // by File.Path
	titles  map[string]string       // by File.Path
	skipped int
	nested  *nestedZip // the zip inside being read
}

type archiveEntry struct {
	container string // the path of the zip holding it in the archive, empty for the archive itself
	name      string // its path in its zip
	gzipped   bool
}

type nestedZip struct {
	container string
	file      *os.File
	zip       *zip.Reader
}

// OpenArchive opens a zip archive of activities. Its id is its absolute path: importing it again
// skips the files already sent without reading them.
func OpenArchive(name string) (*ZipArchive, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	r, err := zip.OpenReader(abs)
	if err != nil {
		return nil, fmt.Errorf("%s is not a zip archive: %w", filepath.Base(abs), err)
	}
	return &ZipArchive{path: abs, id: "archive:" + abs, zip: r}, nil
}

func (a *ZipArchive) ID() string   { return a.id }
func (a *ZipArchive) Name() string { return filepath.Base(a.path) }
func (a *ZipArchive) Kind() Kind   { return Archive }

// Title returns the title of an activity in the activities.csv of a Strava export, empty without
func (a *ZipArchive) Title(f File) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.titles[f.Path]
}

// Skipped returns the number of GPX and TCX files found by Files, not sent: the server reads FIT files
func (a *ZipArchive) Skipped() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.skipped
}

// Close releases the archive and removes the temporary files
func (a *ZipArchive) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closeNested()
	if a.zip != nil {
		a.zip.Close()
		a.zip = nil
	}
}

func (a *ZipArchive) closeNested() {
	if a.nested != nil {
		a.nested.file.Close()
		os.Remove(a.nested.file.Name())
		a.nested = nil
	}
}

func (a *ZipArchive) open() (*zip.Reader, error) {
	if a.zip == nil {
		r, err := zip.OpenReader(a.path)
		if err != nil {
			return nil, err
		}
		a.zip = r
	}
	return &a.zip.Reader, nil
}

// fileKind tells what a file of an archive is
func fileKind(name string) (fit, gzipped, skipped, nested bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".fit"):
		return true, false, false, false
	case strings.HasSuffix(lower, ".fit.gz"):
		return true, true, false, false
	case strings.HasSuffix(lower, ".gpx"), strings.HasSuffix(lower, ".gpx.gz"),
		strings.HasSuffix(lower, ".tcx"), strings.HasSuffix(lower, ".tcx.gz"):
		return false, false, true, false
	case strings.HasSuffix(lower, ".zip"):
		return false, false, false, true
	}
	return false, false, false, false
}

// Files lists the FIT files of the archive, of the zips inside it, and reads the titles of a Strava export
func (a *ZipArchive) Files(ctx context.Context) ([]File, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, err := a.open()
	if err != nil {
		return nil, err
	}
	a.entries, a.titles, a.skipped = map[string]archiveEntry{}, map[string]string{}, 0
	var files []File
	add := func(container string, f *zip.File) {
		fit, gzipped, skipped, _ := fileKind(f.Name)
		switch {
		case skipped:
			a.skipped++
		case fit && !f.FileInfo().IsDir():
			p := f.Name
			if container != "" {
				p = container + nestedSeparator + f.Name
			}
			a.entries[p] = archiveEntry{container: container, name: f.Name, gzipped: gzipped}
			files = append(files, File{Path: p, Size: int64(f.UncompressedSize64), ModTime: f.Modified})
		}
	}
	var titles map[string]string
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, _, _, nested := fileKind(f.Name)
		switch {
		case nested:
			inner, err := a.openNested(f.Name)
			if err != nil {
				// a broken zip inside doesn't stop the others
				a.skipped++
				continue
			}
			for _, g := range inner.File {
				add(f.Name, g)
			}
		case isStravaActivities(f.Name):
			if titles, err = readStravaTitles(f); err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
		default:
			add("", f)
		}
	}
	a.closeNested()
	for p, title := range titles {
		if _, ok := a.entries[p]; ok {
			a.titles[p] = title
		}
	}
	// the files of a zip inside together: each is decompressed once
	sort.SliceStable(files, func(i, j int) bool {
		return a.entries[files[i].Path].container < a.entries[files[j].Path].container
	})
	return files, nil
}

// openNested decompresses a zip of the archive to a temporary file, the previous one removed
func (a *ZipArchive) openNested(container string) (*zip.Reader, error) {
	if a.nested != nil && a.nested.container == container {
		return a.nested.zip, nil
	}
	a.closeNested()
	r, err := a.open()
	if err != nil {
		return nil, err
	}
	var entry *zip.File
	for _, f := range r.File {
		if f.Name == container {
			entry = f
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("%s is not in the archive anymore", container)
	}
	src, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	tmp, err := os.CreateTemp("", "mysportweb-archive-*.zip")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	n, err := io.Copy(tmp, io.LimitReader(src, maxNestedSize+1))
	if err != nil {
		return nil, err
	}
	if n > maxNestedSize {
		return nil, fmt.Errorf("%s is larger than %d GB decompressed", container, maxNestedSize>>30)
	}
	inner, err := zip.NewReader(tmp, n)
	if err != nil {
		return nil, err
	}
	keep = true
	a.nested = &nestedZip{container: container, file: tmp, zip: inner}
	return inner, nil
}

// Copy writes a FIT file of the archive, decompressed, to w. A file that can't be read is an
// *UnreadableError, the archive can still be read.
func (a *ZipArchive) Copy(ctx context.Context, f File, w io.Writer) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.entries[f.Path]
	if !ok {
		return fmt.Errorf("%s is not a file of the archive", f.Path)
	}
	var r *zip.Reader
	var err error
	if entry.container == "" {
		r, err = a.open()
	} else {
		r, err = a.openNested(entry.container)
	}
	if err != nil {
		return err
	}
	var file *zip.File
	for _, candidate := range r.File {
		if candidate.Name == entry.name {
			file = candidate
			break
		}
	}
	if file == nil {
		return fmt.Errorf("%s is not in the archive anymore", f.Path)
	}
	unreadable := func(err error) error { return &UnreadableError{Path: f.Path, Err: err} }
	src, err := file.Open()
	if err != nil {
		return unreadable(err)
	}
	defer src.Close()
	var content io.Reader = src
	if entry.gzipped {
		gz, err := gzip.NewReader(src)
		if err != nil {
			return unreadable(err)
		}
		defer gz.Close()
		content = gz
	}
	n, err := io.Copy(w, io.LimitReader(content, MaxFITSize+1))
	if err != nil {
		return unreadable(err)
	}
	if n > MaxFITSize {
		return unreadable(fmt.Errorf("larger than %d MB, the server refuses it", MaxFITSize>>20))
	}
	return nil
}

// isStravaActivities tells whether a file of an archive is the activities.csv of a Strava export, at its root
func isStravaActivities(name string) bool {
	return strings.EqualFold(name, "activities.csv")
}

// readStravaTitles returns the titles of the activities.csv of a Strava export, by the path of their file
// in the export (its Filename column, "activities/123.fit.gz"). Its first row names the columns; without
// the columns of the file and of the title, there are none.
func readStravaTitles(f *zip.File) (map[string]string, error) {
	src, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	r := csv.NewReader(src)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// the first column of a name: Strava repeats some (Elapsed Time, Distance…)
	column := func(names ...string) int {
		for i, h := range header {
			h = strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))
			for _, name := range names {
				if strings.EqualFold(h, name) {
					return i
				}
			}
		}
		return -1
	}
	fileColumn, titleColumn := column("Filename"), column("Activity Name")
	if fileColumn < 0 || titleColumn < 0 {
		return nil, nil
	}
	titles := map[string]string{}
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			return titles, nil
		}
		if err != nil {
			return nil, err
		}
		if fileColumn >= len(record) || titleColumn >= len(record) {
			continue
		}
		name := strings.TrimSpace(record[fileColumn])
		title := strings.TrimSpace(record[titleColumn])
		if name != "" && title != "" {
			titles[path.Clean(strings.ReplaceAll(name, "\\", "/"))] = title
		}
	}
}

// UploadName is the name a file is sent with: without .gz, it is sent decompressed
func (f File) UploadName() string {
	name := f.Name()
	if strings.HasSuffix(strings.ToLower(name), ".gz") {
		return name[:len(name)-len(".gz")]
	}
	return name
}

package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"MySportWeb/services/activityService/convert"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/muktihari/fit/profile/typedef"
)

// PreparedUpload is an uploaded file made ready for the analysis: a FIT file, where it was uploaded
type PreparedUpload struct {
	Format convert.Format // the format of the upload
	// FITName is the name of the FIT file to store, SourceName the one of the GPX or TCX file it was
	// converted from (empty for a FIT upload)
	FITName    string
	SourceName string
	// SourcePath is where the GPX or TCX file is, decompressed, beside the FIT file: on disk, not in memory,
	// while the upload waits for its analysis and its thumbnail. Empty for a FIT upload. The caller moves it
	// or removes it.
	SourcePath string
}

// SourceSuffix ends the path of the GPX or TCX file of an upload being prepared: the path of the upload
// followed by it
const SourceSuffix = ".source"

// PrepareUpload turns the file uploaded at path into a FIT file, at the same path: a gzipped file is
// decompressed, a GPX or TCX file converted, its sport sport (a FIT sport name, "" for generic) when it
// doesn't name it. An *InvalidFitError for a file that isn't an activity. It holds an analysis slot:
// decompressing (up to convert.MaxDecompressed) and converting take as much memory as analysing, a burst
// of uploads waits for its turn, ErrBusy after QueueTimeout.
func PrepareUpload(path, name, sport string) (PreparedUpload, error) {
	release, err := analysisSlots.acquire(QueueTimeout)
	if err != nil {
		metrics.Analyses.WithLabelValues("busy").Inc()
		return PreparedUpload{}, err
	}
	defer release()
	content, err := os.ReadFile(path)
	if err != nil {
		return PreparedUpload{}, err
	}
	content, err = convert.Gunzip(content)
	if err != nil {
		return PreparedUpload{}, &InvalidFitError{err}
	}
	stem := uploadStem(name)
	prepared := PreparedUpload{Format: convert.Detect(content)}
	switch prepared.Format {
	case convert.FIT:
		prepared.FITName = stem + filepath.Ext(strings.TrimSuffix(filepath.Base(name), ".gz"))
		if !strings.EqualFold(filepath.Ext(prepared.FITName), ".fit") {
			prepared.FITName = stem + ".fit"
		}
	case convert.GPX, convert.TCX:
		result, err := convert.Convert(content, typedef.SportFromString(sport))
		var invalid *convert.InvalidError
		if errors.As(err, &invalid) {
			return PreparedUpload{}, &InvalidFitError{err}
		}
		if err != nil {
			return PreparedUpload{}, err
		}
		prepared.FITName = stem + ".fit"
		prepared.SourceName = stem + "." + string(prepared.Format)
		prepared.SourcePath = path + SourceSuffix
		if err := os.WriteFile(prepared.SourcePath, content, 0o660); err != nil {
			os.Remove(prepared.SourcePath)
			return PreparedUpload{}, err
		}
		content = result.FIT
	default:
		return PreparedUpload{}, &InvalidFitError{errors.New("not a FIT, GPX or TCX file")}
	}
	if err := os.WriteFile(path, content, 0o660); err != nil {
		if prepared.SourcePath != "" {
			os.Remove(prepared.SourcePath)
		}
		return PreparedUpload{}, err
	}
	return prepared, nil
}

// uploadStem returns the name of an uploaded file without its directory, its .gz and its extension
func uploadStem(name string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if strings.HasSuffix(strings.ToLower(base), ".gz") {
		base = base[:len(base)-len(".gz")]
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" || stem == "." || stem == "/" {
		return "activity"
	}
	return stem
}

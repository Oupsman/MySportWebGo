package activityService

import (
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
	// converted from (empty for a FIT upload), Source its content, decompressed
	FITName    string
	SourceName string
	Source     []byte
}

// PrepareUpload turns the file uploaded at path into a FIT file, at the same path: a gzipped file is
// decompressed, a GPX or TCX file converted, its sport sport (a FIT sport name, "" for generic) when it
// doesn't name it. An *InvalidFitError for a file that isn't an activity.
func PrepareUpload(path, name, sport string) (PreparedUpload, error) {
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
		prepared.Source = content
		content = result.FIT
	default:
		return PreparedUpload{}, &InvalidFitError{errors.New("not a FIT, GPX or TCX file")}
	}
	if err := os.WriteFile(path, content, 0o660); err != nil {
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

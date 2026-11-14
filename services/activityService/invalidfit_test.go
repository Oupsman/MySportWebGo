package activityService

import (
	"MySportWeb/internal/pkg/models"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// a file that isn't a FIT activity is refused with an InvalidFitError: an empty one was analysed as an
// empty activity, without error, and saved
func TestAnalyzeRefusesFilesThatAreNotFit(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"empty.fit": "",
		"text.fit":  "these are not the records of a ride",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := AnalyzeTrimmed(path, models.Users{}, models.Equipments{}, 0, nil)
		var invalid *InvalidFitError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: %v, want an InvalidFitError", name, err)
		}
	}
	// the file missing is the server's error, not the user's
	_, err := AnalyzeTrimmed(filepath.Join(dir, "missing.fit"), models.Users{}, models.Equipments{}, 0, nil)
	var invalid *InvalidFitError
	if err == nil || errors.As(err, &invalid) {
		t.Errorf("missing file: %v, want another error", err)
	}
}

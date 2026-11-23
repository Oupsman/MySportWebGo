package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/services/activityService/convert"
	"MySportWeb/services/activityService/convert/converttest"
	"bytes"
	"compress/gzip"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeUpload(t *testing.T, content []byte, gzipped bool) string {
	t.Helper()
	if gzipped {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		w.Write(content)
		w.Close()
		content = buf.Bytes()
	}
	path := filepath.Join(t.TempDir(), "upload-1.fit")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestUploadGPX: a GPX upload is converted, analysed and trimmed as a FIT one
func TestUploadGPX(t *testing.T) {
	original := converttest.GPX(false)
	path := writeUpload(t, original, true)
	prepared, err := PrepareUpload(path, "Morning_Ride.gpx.gz", "cycling")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Format != convert.GPX || prepared.FITName != "Morning_Ride.fit" ||
		prepared.SourceName != "Morning_Ride.gpx" || prepared.SourcePath != path+SourceSuffix {
		t.Errorf("prepared %+v", prepared)
	}
	// the GPX file waits on disk, decompressed
	if source, err := os.ReadFile(prepared.SourcePath); err != nil || !bytes.Equal(source, original) {
		t.Errorf("source kept: %v, the same %v", err, bytes.Equal(source, original))
	}
	user := models.Users{SecurityDistance: 0}
	activity, err := AnalyzeTrimmed(path, user, 0, Analysis{})
	if err != nil {
		t.Fatal(err)
	}
	if activity.Sport != "cycling" || !activity.Date.Equal(converttest.Start) || activity.Duration != 598 {
		t.Errorf("sport %s, date %v, duration %d", activity.Sport, activity.Date, activity.Duration)
	}
	if math.Abs(activity.Distance-2995) > 5 || activity.AvgHR < 144 || activity.AvgHR > 145 {
		t.Errorf("distance %.0f, hr %d", activity.Distance, activity.AvgHR)
	}
	if activity.DeviceName != "StravaGPX iPhone" {
		t.Errorf("device %q", activity.DeviceName)
	}
	if len(activity.GpsPoints) == 0 {
		t.Error("no trace")
	}

	trimmed, err := AnalyzeTrimmed(path, user, 200, Analysis{})
	if err != nil {
		t.Fatal(err)
	}
	if trimmed.Duration != 200 || trimmed.Distance >= activity.Distance/2 {
		t.Errorf("trimmed: %d s, %.0f m", trimmed.Duration, trimmed.Distance)
	}
}

func TestUploadTCXAndFIT(t *testing.T) {
	path := writeUpload(t, converttest.TCX(), false)
	prepared, err := PrepareUpload(path, `C:\exports\run.tcx`, "")
	if err != nil {
		t.Fatal(err)
	}
	activity, err := AnalyzeTrimmed(path, models.Users{}, 0, Analysis{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.SourceName != "run.tcx" || activity.Sport != "running" || activity.Distance != 897 {
		t.Errorf("%s: %s, %.0f m", prepared.SourceName, activity.Sport, activity.Distance)
	}

	// a gzipped FIT file is decompressed, nothing else kept
	fit, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	path = writeUpload(t, fit, true)
	prepared, err = PrepareUpload(path, "ride.FIT.gz", "running")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Format != convert.FIT || prepared.FITName != "ride.FIT" || prepared.SourcePath != "" {
		t.Errorf("FIT: %+v", prepared)
	}
	if again, err := AnalyzeTrimmed(path, models.Users{}, 0, Analysis{}); err != nil || again.Sport != "running" {
		t.Errorf("FIT analysed: %v %v", again.Sport, err)
	}
}

func TestUploadRefused(t *testing.T) {
	for name, content := range map[string][]byte{
		"text":       []byte("hello"),
		"broken gpx": []byte("<gpx><trk>"),
	} {
		path := writeUpload(t, content, false)
		var invalid *InvalidFitError
		if _, err := PrepareUpload(path, "x.gpx", ""); !errors.As(err, &invalid) {
			t.Errorf("%s: %v, want an InvalidFitError", name, err)
		}
		if _, err := os.Stat(path + SourceSuffix); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: source left on disk (%v)", name, err)
		}
	}
}

// decompressing and converting take an analysis slot: with every slot taken, an upload waits, then is
// refused as busy, the file not even read
func TestPrepareUploadWaitsForASlot(t *testing.T) {
	path := writeUpload(t, converttest.GPX(false), true)
	release := fillAnalysisSlots(t)
	if _, err := PrepareUpload(path, "ride.gpx.gz", "cycling"); !errors.Is(err, ErrBusy) {
		t.Fatalf("every slot taken: %v, want ErrBusy", err)
	}
	if _, err := os.Stat(path + SourceSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("busy: source written (%v)", err)
	}
	release()
	prepared, err := PrepareUpload(path, "ride.gpx.gz", "cycling")
	if err != nil || prepared.SourcePath == "" {
		t.Fatalf("a slot free: %+v %v", prepared, err)
	}
	if len(analysisSlots) != 0 {
		t.Errorf("%d slots still taken after the preparation", len(analysisSlots))
	}
}

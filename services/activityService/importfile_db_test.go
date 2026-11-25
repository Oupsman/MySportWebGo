package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/staticmap"
	"MySportWeb/services/activityService/convert/converttest"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// ImportFile, as the background jobs call it: waiting for a free slot instead of ErrBusy, refusing a
// duplicate with the id of the activity the user has
func TestImportFileWaits(t *testing.T) {
	db := emptyDB(t)
	t.Chdir(t.TempDir())
	saved := MapTiles
	MapTiles = staticmap.NewTiles(t.TempDir(), "") // offline: blank tiles
	t.Cleanup(func() { MapTiles = saved })
	user := models.Users{Username: "importer", Email: "importer@test.local", Role: "User"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	newFile := func(content []byte) string {
		t.Helper()
		path, err := CreateImportFile(user.UUID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(path) })
		if err := os.WriteFile(path, content, 0o660); err != nil {
			t.Fatal(err)
		}
		return path
	}

	// the server busy: without Wait, refused at once
	release := fillAnalysisSlots(t)
	_, err := ImportFile(context.Background(), db, user, newFile(converttest.GPX(false)), "ride.gpx", ImportOptions{Sport: "hiking"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("busy, without Wait: %v", err)
	}
	// with Wait, until the context is done
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, err = ImportFile(ctx, db, user, newFile(converttest.GPX(false)), "ride.gpx", ImportOptions{Sport: "hiking", Wait: true})
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("busy, with Wait, context done: %v", err)
	}
	// with Wait, imported once the slots are free
	time.AfterFunc(50*time.Millisecond, release)
	activity, err := ImportFile(context.Background(), db, user, newFile(converttest.GPX(false)), "ride.gpx",
		ImportOptions{Sport: "hiking", Wait: true})
	if err != nil {
		t.Fatalf("with Wait: %v", err)
	}
	stored, err := db.GetActivity(activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Sport != "hiking" || stored.Filename != "ride.gpx" || stored.ThumbnailVersion != ThumbnailVersion {
		t.Errorf("imported: %s, %s, thumbnail %d", stored.Sport, stored.Filename, stored.ThumbnailVersion)
	}
	for _, path := range []string{stored.FilePath, stored.SourceFilePath, ThumbnailPath(user.UUID, stored.ID)} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}

	// the same activity again: the id of the one the user has, nothing left on disk
	path := newFile(converttest.GPX(false))
	_, err = ImportFile(context.Background(), db, user, path, "again.gpx", ImportOptions{Sport: "hiking", Wait: true})
	var duplicate *DuplicateError
	if !errors.As(err, &duplicate) || duplicate.ExistingID != activity.ID {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := os.Stat(path + SourceSuffix); !os.IsNotExist(err) {
		t.Errorf("the GPX file of the duplicate stays: %v", err)
	}

	// not an activity
	_, err = ImportFile(context.Background(), db, user, newFile([]byte("hello")), "x.fit", ImportOptions{Wait: true})
	var invalid *InvalidFitError
	if !errors.As(err, &invalid) {
		t.Errorf("not an activity: %v", err)
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestDeleteActivityFiles(t *testing.T) {
	db := testDB(t)
	// files are relative to the working directory
	t.Chdir(t.TempDir())
	user := models.Users{Username: "files", Email: "files@test.local", UUID: uuid.New()}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	activities := map[string]*models.Activity{}
	create := func(name, fitPath string) {
		a := &models.Activity{ID: uuid.New(), UserID: user.ID, User: user, Date: time.Now(), Sport: "cycling", Title: name, FilePath: fitPath}
		if err := db.Omit("User", "Equipment").Create(a).Error; err != nil {
			t.Fatal(err)
		}
		write(ThumbnailPath(user.UUID, a.ID))
		activities[name] = a
	}
	shared := write(filepath.Join(MediaDir, user.UUID.String(), "Activities", "ride.fit"))
	single := write(filepath.Join(MediaDir, user.UUID.String(), "Activities", "run.fit"))
	outside := write(filepath.Join("elsewhere", "keep.fit"))
	create("first upload", shared)
	create("same file uploaded again", shared)
	create("run", single)
	create("bad path", outside)

	del := func(name string) {
		if err := DeleteActivity(db, activities[name], zerolog.Nop()); err != nil {
			t.Fatal(err)
		}
		if exists(ThumbnailPath(user.UUID, activities[name].ID)) {
			t.Errorf("%s: thumbnail not removed", name)
		}
	}
	del("first upload")
	if !exists(shared) {
		t.Fatal("FIT file removed while another activity uses it")
	}
	del("same file uploaded again")
	if exists(shared) {
		t.Error("FIT file of the last activity using it not removed")
	}
	if !exists(single) {
		t.Error("FIT file of another activity removed")
	}
	del("run")
	if exists(single) {
		t.Error("FIT file not removed")
	}
	del("bad path")
	if !exists(outside) {
		t.Error("file outside of the user's media directory removed")
	}
	var count int64
	db.Model(&models.Activity{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 0 {
		t.Errorf("%d activities left", count)
	}
}

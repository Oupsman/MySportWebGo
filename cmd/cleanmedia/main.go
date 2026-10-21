// cleanmedia finds the files of MEDIA/ that no activity uses any more: thumbnails of deleted activities,
// FIT files of deleted activities or of failed uploads. It only reports them, unless -delete is given.
//
// Usage, from the backend directory (it reads the database configuration in .env):
//
//	go run ./cmd/cleanmedia            report the orphan files
//	go run ./cmd/cleanmedia -delete    remove them
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const mediaDir = "MEDIA"

type orphan struct {
	path string
	size int64
}

func main() {
	remove := flag.Bool("delete", false, "remove the orphan files (default: only report them)")
	flag.Parse()
	if err := run(*remove); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(remove bool) error {
	env, err := godotenv.Read(".env")
	if err != nil {
		return fmt.Errorf("reading .env (run from the backend directory): %w", err)
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		env["DBHOST"], env["DBUSER"], env["DBPASSWORD"], env["DBNAME"], env["DBPORT"])
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}

	var users []struct{ UUID string }
	if err := db.Table("users").Select("uuid").Scan(&users).Error; err != nil {
		return err
	}
	var activities []struct {
		ID       string
		UserUUID string
		FilePath string
	}
	err = db.Table("activities").Select("activities.id, users.uuid AS user_uuid, activities.file_path").
		Joins("JOIN users ON users.id = activities.user_id").Scan(&activities).Error
	if err != nil {
		return err
	}
	// a wrong .env would make every file look orphan
	if len(activities) == 0 && remove {
		return fmt.Errorf("database %s has no activity: refusing to delete, check .env", env["DBNAME"])
	}
	knownUsers := map[string]bool{}
	for _, u := range users {
		knownUsers[u.UUID] = true
	}
	thumbnails := map[string]bool{} // user uuid/activity id
	fitFiles := map[string]bool{}
	for _, a := range activities {
		thumbnails[a.UserUUID+"/"+a.ID] = true
		if a.FilePath != "" {
			fitFiles[filepath.Clean(a.FilePath)] = true
		}
	}

	dirs, err := os.ReadDir(mediaDir)
	if err != nil {
		return err
	}
	var orphanThumbnails, orphanFits []orphan
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		userUUID := dir.Name()
		if !knownUsers[userUUID] {
			fmt.Printf("skipped %s: no such user in database %s\n", filepath.Join(mediaDir, userUUID), env["DBNAME"])
			continue
		}
		orphanThumbnails = append(orphanThumbnails, find(filepath.Join(mediaDir, userUUID, "thumbnails"), func(name, _ string) bool {
			return strings.HasSuffix(name, ".png") && !thumbnails[userUUID+"/"+strings.TrimSuffix(name, ".png")]
		})...)
		orphanFits = append(orphanFits, find(filepath.Join(mediaDir, userUUID, "Activities"), func(_, path string) bool {
			return !fitFiles[path]
		})...)
	}

	fmt.Printf("database %s: %d activities, %d FIT files used\n", env["DBNAME"], len(activities), len(fitFiles))
	report("thumbnails without activity", orphanThumbnails)
	report("FIT files without activity", orphanFits)
	if !remove {
		if len(orphanThumbnails)+len(orphanFits) > 0 {
			fmt.Println("nothing removed, run with -delete to remove them")
		}
		return nil
	}
	removed := 0
	for _, o := range append(orphanThumbnails, orphanFits...) {
		if err := os.Remove(o.path); err != nil {
			fmt.Fprintln(os.Stderr, "not removed:", err)
			continue
		}
		removed++
	}
	fmt.Printf("%d files removed\n", removed)
	return nil
}

// find returns the regular files of dir for which isOrphan is true
func find(dir string, isOrphan func(name, path string) bool) []orphan {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var orphans []orphan
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if isOrphan(e.Name(), path) {
			orphans = append(orphans, orphan{path, info.Size()})
		}
	}
	return orphans
}

func report(what string, orphans []orphan) {
	var size int64
	for _, o := range orphans {
		size += o.size
	}
	fmt.Printf("%d %s (%.1f MB)\n", len(orphans), what, float64(size)/1e6)
	for i, o := range orphans {
		if i == 5 {
			fmt.Printf("  ... and %d more\n", len(orphans)-5)
			break
		}
		fmt.Println("  " + o.path)
	}
}

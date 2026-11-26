package activityService

import (
	"MySportWeb/internal/pkg/models"
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportUser(t *testing.T) {
	db := emptyDB(t)
	t.Chdir(t.TempDir())
	user := models.Users{Username: "exporter", Email: "exporter@test.local", Password: "secret hash",
		TOTPSecret: "secret totp"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	ride := saveRide(t, db, user, "cycling", day, 200, 200)
	dir := filepath.Join(UserMediaDir(user.UUID), "Activities")
	os.MkdirAll(dir, 0o700)
	fit, gpx := filepath.Join(dir, "ride.fit"), filepath.Join(dir, "ride.gpx")
	os.WriteFile(fit, []byte("FIT bytes"), 0o600)
	os.WriteFile(gpx, []byte("<gpx/>"), 0o600)
	if err := db.Model(&ride).Updates(map[string]interface{}{"file_path": fit, "source_file_path": gpx,
		"filename": "ride.gpx"}).Error; err != nil {
		t.Fatal(err)
	}
	saveRide(t, db, user, "running", day.AddDate(0, 0, 1), 250, 250) // its file lost: skipped
	bike := models.Equipments{Name: "bike", UserID: int(user.ID), DefaultSports: []string{"cycling"}}
	if err := db.CreateEquipment(&bike); err != nil {
		t.Fatal(err)
	}
	picture, err := SaveEquipmentPicture(user.UUID, bike.ID, []byte("JPEG bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetEquipmentPicture(bike.ID, picture); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDashboardLayout(user.ID, []models.DashboardWidget{{ID: "calendar", W: 6, H: 2}}); err != nil {
		t.Fatal(err)
	}
	from, to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	for _, row := range []interface{}{
		&models.Services{EquipmentID: bike.ID, Date: day, Mileage: 100, Description: "chain"},
		&models.HealthData{UserID: user.ID, Date: day, Weight: 70},
		&models.Goal{UserID: user.ID, Name: "june", Metric: "distance", Target: 500000, Period: "custom",
			Sports: models.SportList{"cycling"}, StartsOn: &from, EndsOn: &to},
		&models.Workplace{UserID: user.ID, Name: "office", Lat: 45.19, Lon: 5.72, Diameter: 300},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := ExportUser(context.Background(), db, user, &buf); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		content, _ := io.ReadAll(r)
		r.Close()
		files[f.Name] = string(content)
	}
	prefix := "2026-05-01_080000_" + ride.ID.String()[:8]
	for _, name := range []string{"README.txt", "manifest.json", "goals.json", "profile.json", "activities.csv", "health.json", "equipments.json",
		"devices.json", "routes.json", "segments.json", "workplaces.json", "activities/" + prefix + ".json"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if files["files/"+prefix+"_ride.fit"] != "FIT bytes" || files["files/"+prefix+"_ride.gpx"] != "<gpx/>" {
		t.Errorf("uploaded files: %v", files)
	}
	// no secret anywhere
	for name, content := range files {
		if strings.Contains(content, "secret hash") || strings.Contains(content, "secret totp") {
			t.Errorf("%s holds a secret", name)
		}
	}
	rows, err := csv.NewReader(strings.NewReader(files["activities.csv"])).ReadAll()
	if err != nil || len(rows) != 3 || rows[1][2] != "cycling" || rows[1][12] != "files/"+prefix+"_ride.fit" ||
		rows[2][12] != "" {
		t.Errorf("activities.csv: %v %v", rows, err)
	}
	var activity map[string]interface{}
	json.Unmarshal([]byte(files["activities/"+prefix+".json"]), &activity)
	if activity["file_path"] != "files/"+prefix+"_ride.fit" || activity["powers"] == nil {
		t.Errorf("activity: file %v, powers %v", activity["file_path"], activity["powers"] != nil)
	}
	if !strings.Contains(files["equipments.json"], `"chain"`) || !strings.Contains(files["equipments.json"], `"cycling"`) {
		t.Errorf("equipments.json: %s", files["equipments.json"])
	}
	var manifest ExportManifest
	if err := json.Unmarshal([]byte(files["manifest.json"]), &manifest); err != nil || manifest.Format != ExportFormat ||
		manifest.Username != "exporter" {
		t.Errorf("manifest: %+v %v", manifest, err)
	}
	var goals []ExportGoal
	if err := json.Unmarshal([]byte(files["goals.json"]), &goals); err != nil || len(goals) != 1 ||
		goals[0].Name != "june" || goals[0].StartsOn == nil || *goals[0].StartsOn != "2026-06-01" ||
		goals[0].EndsOn == nil || *goals[0].EndsOn != "2026-06-30" || len(goals[0].Sports) != 1 {
		t.Errorf("goals.json: %s %v", files["goals.json"], err)
	}
	var workplaces []models.Workplace
	if err := json.Unmarshal([]byte(files["workplaces.json"]), &workplaces); err != nil || len(workplaces) != 1 ||
		workplaces[0].Name != "office" || workplaces[0].Diameter != 300 || workplaces[0].Lat != 45.19 {
		t.Errorf("workplaces.json: %s %v", files["workplaces.json"], err)
	}
	var equipments []struct {
		ID          string `json:"id"`
		PictureFile string `json:"picture_file"`
	}
	json.Unmarshal([]byte(files["equipments.json"]), &equipments)
	if len(equipments) != 1 || equipments[0].PictureFile != "pictures/"+bike.ID.String()+".jpg" ||
		files[equipments[0].PictureFile] != "JPEG bytes" {
		t.Errorf("picture: %+v", equipments)
	}
	var profile map[string]interface{}
	json.Unmarshal([]byte(files["profile.json"]), &profile)
	if layout, _ := json.Marshal(profile["dashboard_layout"]); profile["username"] != "exporter" ||
		profile["email"] != "exporter@test.local" || !strings.Contains(string(layout), `"calendar"`) {
		t.Errorf("profile: %v", profile)
	}
}

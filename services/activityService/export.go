package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// exportReadme explains the layout of an export
const exportReadme = `MySportWeb export
=================

Everything MySportWeb keeps about you, on %s.

manifest.json           the version of this export's format, and of MySportWeb that made it
profile.json            your account (no password nor second factor secret) and your dashboard layout
activities.csv          one line per activity: its date, sport, title, distance, duration...
activities/*.json       each activity with everything the analysis computed: every recorded series, the trace,
                        the zones, the best efforts, the power curve...
files/                  the files you uploaded: FIT, and the GPX and TCX files the FIT files were converted from
health.json             your weights, resting heart rates, blood pressures...
equipments.json         your equipment, with their services, maintenance reminders and the sports they are the default of
pictures/               the pictures of your equipment, named in equipments.json (picture_file)
goals.json              your goals
workplaces.json         your workplaces: an activity starting or ending in one is a commute
devices.json            the devices your activities were recorded with
routes.json             the routes your activities follow
segments.json           your segments, with the passes of your activities on them
social.json             the cheers and comments you gave, those of the others on your activities, the users you
                        follow and your followers (by their username); an import doesn't restore them

The dates are in UTC (RFC 3339), the distances in meters, the durations in seconds, the speeds in m/s.
`

// ExportFormat is the version of the layout of the exports: an import refuses a newer one. 1 is the exports
// without manifest.json.
const ExportFormat = 2

// ExportManifest is manifest.json, what an import reads first
type ExportManifest struct {
	Format     int       `json:"format"`
	AppVersion string    `json:"app_version"`
	ExportedAt time.Time `json:"exported_at"`
	Username   string    `json:"username"`
}

// ExportGoal is a goal in goals.json: its dates are hidden from the JSON of the model
type ExportGoal struct {
	models.Goal
	StartsOn *string `json:"starts_on"` // YYYY-MM-DD, nil for a goal of every period
	EndsOn   *string `json:"ends_on"`
}

// ExportUser writes the zip of everything a user owns to w: their original files and every computed value,
// as JSON (see exportReadme). The activities are read one at a time: the memory doesn't grow with them.
func ExportUser(ctx context.Context, db *models.DB, user models.Users, w io.Writer) error {
	z := zip.NewWriter(w)
	addJSON := func(name string, v interface{}) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	readme, err := z.Create("README.txt")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	fmt.Fprintf(readme, exportReadme, now.Format(time.RFC3339))
	if err := addJSON("manifest.json", ExportManifest{Format: ExportFormat, AppVersion: vars.Version,
		ExportedAt: now, Username: user.Username}); err != nil {
		return err
	}

	layout, err := db.GetDashboardLayout(user.ID)
	if err != nil {
		return err
	}
	if err := addJSON("profile.json", map[string]interface{}{
		"username": user.Username, "email": user.Email, "role": user.Role, "created_at": user.CreatedAt,
		"date_of_birth": user.DateOfBirth, "gender": user.Gender, "weight": user.Weight, "height": user.Height,
		"max_hr": user.MaxHR, "ftp": user.FTP, "weight_objective": user.WeightObjective, "unit": user.Unit,
		"timezone": user.Timezone, "home_town": user.HomeTown, "security_distance": user.SecurityDistance,
		"two_factor_enabled_at": user.TOTPEnabledAt, "dashboard_layout": layout,
	}); err != nil {
		return err
	}

	var ids []uuid.UUID
	if err := db.Model(&models.Activity{}).Where("user_id = ?", user.ID).Order("date").Pluck("id", &ids).Error; err != nil {
		return err
	}
	var rows [][]string
	rows = append(rows, []string{"id", "date", "sport", "title", "distance_m", "duration_s", "ascent_m",
		"avg_hr", "avg_power", "training_load", "equipment", "device", "file"})
	type upload struct{ from, to string }
	var uploads []upload
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		a, err := db.GetActivity(id)
		if err != nil {
			return err
		}
		prefix := a.Date.UTC().Format("2006-01-02_150405") + "_" + a.ID.String()[:8]
		file := ""
		for _, p := range []string{a.FilePath, a.SourceFilePath} {
			if p == "" {
				continue
			}
			name := "files/" + prefix + "_" + filepath.Base(p)
			if file == "" {
				file = name
			}
			uploads = append(uploads, upload{p, name})
		}
		trimp := ""
		if a.Trimp != nil {
			trimp = strconv.FormatFloat(*a.Trimp, 'f', 1, 64)
		}
		rows = append(rows, []string{a.ID.String(), a.Date.UTC().Format(time.RFC3339), a.Sport, a.Title,
			strconv.FormatFloat(a.Distance, 'f', 1, 64), strconv.FormatUint(uint64(a.Duration), 10),
			strconv.FormatUint(uint64(a.TotalAscent), 10), strconv.FormatUint(uint64(a.AvgHR), 10),
			strconv.FormatUint(uint64(a.AvgPower), 10), trimp, a.Equipment.Name, a.DeviceName, file})
		// the account is in profile.json, not in each activity
		a.User = models.Users{}
		a.Filename, a.FilePath, a.SourceFilePath = filepath.Base(a.Filename), file, ""
		if err := addJSON("activities/"+prefix+".json", a); err != nil {
			return err
		}
	}
	// the summary once the activities are read: an entry of a zip is written before the next one is created
	summary, err := z.Create("activities.csv")
	if err != nil {
		return err
	}
	if err := csv.NewWriter(summary).WriteAll(rows); err != nil {
		return err
	}
	health, err := db.GetHealthDatas(user.ID)
	if err != nil {
		return err
	}
	if err := addJSON("health.json", health); err != nil {
		return err
	}
	equipments, err := db.GetEquipmentsStats(user.ID)
	if err != nil {
		return err
	}
	type equipmentExport struct {
		models.EquipmentStats
		Services []models.Services `json:"services"`
		// PictureFile is its picture in the zip, empty without one
		PictureFile string `json:"picture_file"`
	}
	exported := make([]equipmentExport, len(equipments))
	for i, e := range equipments {
		services, err := db.GetServices(e.ID)
		if err != nil {
			return err
		}
		exported[i] = equipmentExport{EquipmentStats: e, Services: services}
		if from := equipmentPictureFile(user.UUID, e.Picture); from != "" {
			exported[i].PictureFile = "pictures/" + e.ID.String() + filepath.Ext(from)
			uploads = append(uploads, upload{from, exported[i].PictureFile})
		}
	}
	if err := addJSON("equipments.json", exported); err != nil {
		return err
	}
	devices, err := db.GetDevicesStats(user.ID)
	if err != nil {
		return err
	}
	if err := addJSON("devices.json", devices); err != nil {
		return err
	}
	var routes []models.Route
	if err := db.Where("user_id = ?", user.ID).Order("created_at").Find(&routes).Error; err != nil {
		return err
	}
	if err := addJSON("routes.json", routes); err != nil {
		return err
	}
	var segments []models.Segment
	if err := db.Where("user_id = ?", user.ID).Order("created_at").Find(&segments).Error; err != nil {
		return err
	}
	type segmentExport struct {
		models.Segment
		Passes []models.SegmentEffort `json:"passes"`
	}
	exportedSegments := make([]segmentExport, len(segments))
	for i, s := range segments {
		var passes []models.SegmentEffort
		if err := db.Where("segment_id = ?", s.ID).Find(&passes).Error; err != nil {
			return err
		}
		exportedSegments[i] = segmentExport{Segment: s, Passes: passes}
	}
	if err := addJSON("segments.json", exportedSegments); err != nil {
		return err
	}
	var goals []models.Goal
	if err := db.Where("user_id = ?", user.ID).Order("created_at, id").Find(&goals).Error; err != nil {
		return err
	}
	exportedGoals := make([]ExportGoal, len(goals))
	for i, g := range goals {
		exportedGoals[i] = ExportGoal{Goal: g, StartsOn: dateString(g.StartsOn), EndsOn: dateString(g.EndsOn)}
	}
	if err := addJSON("goals.json", exportedGoals); err != nil {
		return err
	}
	workplaces, err := db.GetWorkplaces(user.ID)
	if err != nil {
		return err
	}
	if err := addJSON("workplaces.json", workplaces); err != nil {
		return err
	}
	social, err := db.GetSocialExport(user.ID)
	if err != nil {
		return err
	}
	if err := addJSON("social.json", social); err != nil {
		return err
	}
	// the files last: they are the bulk of the zip
	for _, u := range uploads {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := addFile(z, u.from, u.to); err != nil {
			return err
		}
	}
	return z.Close()
}

// dateString is a date as YYYY-MM-DD, nil for nil
func dateString(date *time.Time) *string {
	if date == nil {
		return nil
	}
	s := date.Format(time.DateOnly)
	return &s
}

// equipmentPictureFile is the file of the picture of an equipment, from its URL path (SaveEquipmentPicture);
// empty without picture, or for a path outside the directory of the user
func equipmentPictureFile(userUUID uuid.UUID, urlPath string) string {
	if urlPath == "" {
		return ""
	}
	path := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(urlPath, "/")))
	if !strings.HasPrefix(path, filepath.Clean(UserMediaDir(userUUID))+string(filepath.Separator)) {
		return ""
	}
	return path
}

// addFile copies a file of the user into the zip; a missing one (lost from the disk) is skipped
func addFile(z *zip.Writer, from, to string) error {
	f, err := os.Open(from)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name, header.Method = to, zip.Deflate
	w, err := z.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

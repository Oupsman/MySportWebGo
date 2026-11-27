package apicontrollers

// The import of the zip of an export (activityService.ExportUser) into the account of the user: the
// account is completed, never overwritten. What it already has (an activity at the same date, an equipment
// of the same name...) is kept as it is and counted as existing: importing the same zip again changes
// nothing. The activities are analysed again from their files, then given back what the user set (title,
// equipment, visibility, trim...). The data of the zip goes through the checks of the API (the inputs of the
// goals, reminders, services, profile): a zip is the user's input.
//
// The upload stores the zip, checks it, and answers at once; the import runs in the background, one at a
// time per user, its progress and its report read with GET /user/import. A stop of the server interrupts it,
// the next start runs it again from the start: what was imported is found as existing.

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/notifications"
	"MySportWeb/internal/pkg/pictures"
	"MySportWeb/internal/pkg/types"
	"MySportWeb/services/activityService"
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The limits of the zip of an export: each entry is checked against them before anything is read, and the
// zip reader refuses an entry larger than it says
const (
	maxImportEntries = 200_000
	// maxImportJSON is the largest JSON file: an activity of a day with every series is about 20 MB
	maxImportJSON = 64 << 20
	// maxImportFile is the largest file of an activity: a GPX file of a long day, decompressed
	maxImportFile = 128 << 20
	// healthImportChunk is the number of health measures saved per transaction
	healthImportChunk = 5000
)

// importTopFiles are the files at the root of an export
var importTopFiles = map[string]bool{"README.txt": true, "manifest.json": true, "profile.json": true,
	"activities.csv": true, "health.json": true, "equipments.json": true, "devices.json": true, "routes.json": true,
	"segments.json": true, "goals.json": true, "workplaces.json": true}

// importDirs are the directories of an export and the largest file of each
var importDirs = map[string]int64{"activities/": maxImportJSON, "files/": maxImportFile,
	"pictures/": pictures.MaxUploadSize}

// errNotExport is a zip that isn't an export of MySportWeb
var errNotExport = errors.New("this zip isn't an export of MySportWeb: it has neither manifest.json nor profile.json")

// invalidExportError is a zip refused by openExport: its message is for the user
type invalidExportError struct{ err error }

func (e *invalidExportError) Error() string { return e.err.Error() }
func (e *invalidExportError) Unwrap() error { return e.err }

// exportZip is the zip of an export, checked
type exportZip struct {
	reader *zip.ReadCloser
	files  map[string]*zip.File
	format int
	// activities are the activities/*.json, in the order of their names: their date, the oldest first
	activities []string
	// storage is the size of the files of the activities and the pictures: what the import stores
	storage int64
}

// openExport opens and checks the zip of an export at path: an *invalidExportError for a zip that can't be
// one. The entries it doesn't know are ignored, never read: a zip made again by hand may have more.
func openExport(path string) (*exportZip, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, &invalidExportError{fmt.Errorf("the file isn't a readable zip: %w", err)}
	}
	e := &exportZip{reader: reader, files: map[string]*zip.File{}}
	if err := e.check(); err != nil {
		reader.Close()
		return nil, &invalidExportError{err}
	}
	return e, nil
}

func (e *exportZip) check() error {
	if len(e.reader.File) > maxImportEntries {
		return fmt.Errorf("the zip has more than %d files", maxImportEntries)
	}
	for _, f := range e.reader.File {
		limit, known := importEntryLimit(f.Name)
		if !known || strings.HasSuffix(f.Name, "/") {
			continue
		}
		if _, twice := e.files[f.Name]; twice {
			return fmt.Errorf("%s is twice in the zip", f.Name)
		}
		if f.UncompressedSize64 > uint64(limit) {
			return fmt.Errorf("%s is too large: %d MB at most", f.Name, limit>>20)
		}
		e.files[f.Name] = f
		switch {
		case strings.HasPrefix(f.Name, "activities/") && strings.HasSuffix(f.Name, ".json"):
			e.activities = append(e.activities, f.Name)
		case strings.HasPrefix(f.Name, "files/"), strings.HasPrefix(f.Name, "pictures/"):
			e.storage += int64(f.UncompressedSize64)
		}
	}
	sort.Strings(e.activities)
	var manifest activityService.ExportManifest
	found, err := e.decode("manifest.json", &manifest)
	switch {
	case err != nil:
		return err
	case found && manifest.Format > activityService.ExportFormat:
		return fmt.Errorf("this export was made by a newer version of MySportWeb (%s, format %d): upgrade this "+
			"instance to import it", manifest.AppVersion, manifest.Format)
	case found && manifest.Format < 2:
		return errors.New("manifest.json has no valid format")
	case found:
		e.format = manifest.Format
	case e.files["profile.json"] == nil:
		return errNotExport
	default:
		// the exports before the manifest
		e.format = 1
	}
	return nil
}

// importEntryLimit returns the largest size of an entry of an export, false for an entry that isn't one of
// an export: another directory, a sub-directory
func importEntryLimit(name string) (int64, bool) {
	if importTopFiles[name] {
		return maxImportJSON, true
	}
	for dir, limit := range importDirs {
		rest, ok := strings.CutPrefix(name, dir)
		if !ok {
			continue
		}
		if rest == "" || rest == "." || rest == ".." || strings.ContainsAny(rest, "/\\") {
			return 0, false
		}
		return limit, true
	}
	return 0, false
}

func (e *exportZip) Close() error { return e.reader.Close() }

// decode reads the JSON file name of the zip into v; false when the zip doesn't have it
func (e *exportZip) decode(name string, v interface{}) (bool, error) {
	f := e.files[name]
	if f == nil {
		return false, nil
	}
	r, err := f.Open()
	if err != nil {
		return true, fmt.Errorf("%s: %w", name, err)
	}
	defer r.Close()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		return true, fmt.Errorf("%s isn't valid: %w", name, err)
	}
	return true, nil
}

// extract copies the file name of the zip to the file at to
func (e *exportZip) extract(name, to string) error {
	f := e.files[name]
	if f == nil {
		return fmt.Errorf("%s isn't in the export", name)
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(to, os.O_WRONLY|os.O_TRUNC, 0o660)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// The imports running in this process: their cancel, for DELETE /user/import
var runningImports = struct {
	sync.Mutex
	cancels map[uuid.UUID]context.CancelFunc
}{cancels: map[uuid.UUID]context.CancelFunc{}}

// importsDir is the directory of the zips of the imports of a user: in their directory, counted in their
// storage while it is imported, never served (ServeMedia), never taken for an orphan (cleanmedia)
func importsDir(userUUID uuid.UUID) string {
	return filepath.Join(activityService.UserMediaDir(userUUID), "imports")
}

// ImportAccount stores the zip of an export (multipart: file, and profile=true to import the settings of the
// profile too), checks it, and starts its import in the background: 202 with the import. 400 for a zip that
// isn't an export, 409 while another import runs, 413 above IMPORT_MAX_SIZE, 507 when the files of the
// activities don't fit in the storage quota.
func ImportAccount(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	db := &App.DB
	user := middlewares.CurrentUser(c)
	// before reading a large upload for nothing; checked again when saving
	if latest, err := db.GetLatestAccountImport(user.ID); err == nil &&
		(latest.Status == models.ImportPending || latest.Status == models.ImportRunning) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": models.ErrImportRunning.Error()})
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		internalError(c, err)
		return
	}
	// a zip of a few GB over a home connection takes longer than ReadTimeout: the size is bounded by its
	// body limit (IMPORT_MAX_SIZE), and only a logged in user can send it
	controller := http.NewResponseController(c.Writer)
	if err := errors.Join(controller.SetReadDeadline(time.Time{}), controller.SetWriteDeadline(time.Time{})); err != nil {
		App.Logger.Warn().Err(err).Msg("import: the deadlines can't be lifted")
	}
	// read as it comes, straight to its file: not in memory, nor in the temporary directory
	parts, err := c.Request.MultipartReader()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "send the zip as multipart/form-data, field file"})
		return
	}
	imp := models.AccountImport{ID: uuid.New(), UserID: user.ID}
	if err := os.MkdirAll(importsDir(user.UUID), 0o770); err != nil {
		internalError(c, err)
		return
	}
	imp.ZipPath = filepath.Join(importsDir(user.UUID), imp.ID.String()+".zip")
	started := false
	defer func() {
		if !started {
			os.Remove(imp.ZipPath)
		}
	}()
	stored := false
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if !middlewares.RequestTooLarge(c, err) {
				badRequest(c, err)
			}
			return
		}
		switch part.FormName() {
		case "profile":
			value, _ := io.ReadAll(io.LimitReader(part, 16))
			imp.WithProfile = strings.TrimSpace(string(value)) == "true"
		case "file":
			if stored {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "one zip at a time"})
				return
			}
			if err := storeUpload(part, imp.ZipPath); err != nil {
				if !middlewares.RequestTooLarge(c, err) {
					internalError(c, err)
				}
				return
			}
			stored = true
		}
	}
	if !stored {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}
	export, err := openExport(imp.ZipPath)
	if err != nil {
		var invalid *invalidExportError
		if errors.As(err, &invalid) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		internalError(c, err)
		return
	}
	imp.Format, imp.Total = export.format, len(export.activities)
	storage := export.storage
	export.Close()
	// the zip is removed once imported: what stays is the files of the activities and the pictures
	info, err := os.Stat(imp.ZipPath)
	if err != nil {
		internalError(c, err)
		return
	}
	if !withinQuota(c, user, storage-info.Size()) {
		return
	}
	imp.Report = &models.ImportReport{}
	if err := db.CreateAccountImport(&imp); err != nil {
		if errors.Is(err, models.ErrImportRunning) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		internalError(c, err)
		return
	}
	started = true
	startAccountImport(App, imp)
	App.Logger.Info().Uint("user_id", user.ID).Str("import", imp.ID.String()).Int("activities", imp.Total).
		Msg("import of an export started")
	recordEvent(c, user, models.EventAccountImport, "")
	c.JSON(http.StatusAccepted, imp)
}

// storeUpload writes r to the new file at to
func storeUpload(r io.Reader, to string) error {
	w, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o660)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// GetAccountImport answers the latest import of the user, its progress and its report; 404 without
func GetAccountImport(c *gin.Context) {
	db := c.MustGet("App").(*app.App).DB
	imp, err := db.GetLatestAccountImport(middlewares.CurrentUser(c).ID)
	if err != nil {
		lookupError(c, err, "import")
		return
	}
	c.JSON(http.StatusOK, imp)
}

// CancelAccountImport stops the import of the user after the item it is on: what it imported stays.
// 409 when no import runs.
func CancelAccountImport(c *gin.Context) {
	App := c.MustGet("App").(*app.App)
	imp, err := App.DB.GetLatestAccountImport(middlewares.CurrentUser(c).ID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		internalError(c, err)
		return
	}
	if err != nil || (imp.Status != models.ImportPending && imp.Status != models.ImportRunning) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "no import is running"})
		return
	}
	if _, err := App.DB.FinishAccountImport(&imp, models.ImportCancelled, ""); err != nil {
		internalError(c, err)
		return
	}
	runningImports.Lock()
	cancel := runningImports.cancels[imp.ID]
	runningImports.Unlock()
	if cancel != nil {
		// the import removes its zip when it stops
		cancel()
	} else {
		os.Remove(imp.ZipPath)
	}
	c.JSON(http.StatusOK, imp)
}

// ResumeAccountImports runs again the imports a stop of the server interrupted. At the start, once App.Start
// ran.
func ResumeAccountImports(App *app.App) {
	imports, err := App.DB.GetUnfinishedAccountImports()
	if err != nil {
		App.Logger.Error().Err(err).Msg("the imports interrupted by the last stop can't be read: not resumed")
		return
	}
	for _, imp := range imports {
		App.Logger.Info().Uint("user_id", imp.UserID).Str("import", imp.ID.String()).Msg("import of an export resumed")
		startAccountImport(App, imp)
	}
}

// startAccountImport runs an import in the background
func startAccountImport(App *app.App, imp models.AccountImport) {
	App.Go(func(shutdown context.Context) {
		ctx, cancel := context.WithCancel(shutdown)
		defer cancel()
		runningImports.Lock()
		runningImports.cancels[imp.ID] = cancel
		runningImports.Unlock()
		defer func() {
			runningImports.Lock()
			delete(runningImports.cancels, imp.ID)
			runningImports.Unlock()
		}()
		runAccountImport(ctx, shutdown, App, imp)
	})
}

// accountImport is an import running
type accountImport struct {
	App    *app.App
	db     *models.DB
	user   models.Users
	imp    *models.AccountImport
	export *exportZip
	report *models.ImportReport
	// the ids of the export, and the ones of the account for them (created, or found existing)
	equipments map[uuid.UUID]uuid.UUID
	reminders  map[uuid.UUID]uuid.UUID
	activities map[uuid.UUID]uuid.UUID
	// created are the activities this import created: the ones it may change
	created map[uuid.UUID]bool
	// routeActivities are the activities of each route of the export
	routeActivities map[uuid.UUID][]uuid.UUID
	ridesImported   bool
}

// runAccountImport imports the zip of imp; ctx is done when the user cancels it, shutdown when the server
// stops (the import then stays running, the next start runs it again)
func runAccountImport(ctx, shutdown context.Context, App *app.App, imp models.AccountImport) {
	db, logger := &App.DB, App.Logger.With().Uint("user_id", imp.UserID).Str("import", imp.ID.String()).Logger()
	fail := func(err error) {
		logger.Error().Err(err).Msg("import of an export failed")
		if _, ferr := db.FinishAccountImport(&imp, models.ImportFailed, err.Error()); ferr != nil {
			logger.Error().Err(ferr).Msg("failed import not saved")
		}
		os.Remove(imp.ZipPath)
	}
	user, err := db.GetUser(uint64(imp.UserID))
	if err != nil {
		fail(fmt.Errorf("the account can't be read: %w", err))
		return
	}
	export, err := openExport(imp.ZipPath)
	if err != nil {
		fail(err)
		return
	}
	defer export.Close()
	// a resumed import starts its counts again: what it imported before is found as existing
	imp.Status, imp.Done, imp.Report = models.ImportRunning, 0, &models.ImportReport{}
	if saved, err := db.SaveAccountImportProgress(&imp); err != nil || !saved {
		if err != nil {
			logger.Error().Err(err).Msg("import not started")
		}
		return
	}
	j := &accountImport{App: App, db: db, user: user, imp: &imp, export: export, report: imp.Report,
		equipments: map[uuid.UUID]uuid.UUID{}, reminders: map[uuid.UUID]uuid.UUID{},
		activities: map[uuid.UUID]uuid.UUID{}, created: map[uuid.UUID]bool{},
		routeActivities: map[uuid.UUID][]uuid.UUID{}}
	steps := []struct {
		name string
		run  func(ctx context.Context) error
	}{
		{"profile", j.importProfile},
		// before the activities: the altitude correction of their devices applies to their analysis
		{"devices", j.importDevices},
		// before the activities: their equipment, and the default equipment of their sport
		{"equipments", j.importEquipments},
		// before the activities: the weights and resting heart rates of their analysis
		{"health", j.importHealth},
		{"activities", j.importActivities},
		{"routes", j.importRoutes},
		// after the activities: their passes are found when the segment is created
		{"segments", j.importSegments},
		{"goals", j.importGoals},
		// after the activities: their commute flags are the ones of the export
		{"workplaces", j.importWorkplaces},
	}
	for _, step := range steps {
		err := step.run(ctx)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			continue
		}
		switch {
		case shutdown.Err() != nil:
			logger.Info().Str("step", step.name).Msg("import of an export stopped by the shutdown: resumed at the next start")
		case ctx.Err() != nil, errors.Is(err, context.Canceled):
			// cancelled (here, or found cancelled when saving its progress): the status is saved by
			// CancelAccountImport
			logger.Info().Str("step", step.name).Msg("import of an export cancelled")
			os.Remove(imp.ZipPath)
		default:
			fail(fmt.Errorf("%s: %w", step.name, err))
		}
		return
	}
	imp.Done = imp.Total
	if finished, err := db.FinishAccountImport(&imp, models.ImportDone, ""); err != nil {
		logger.Error().Err(err).Msg("finished import not saved")
	} else if !finished {
		return
	}
	os.Remove(imp.ZipPath)
	// the estimated FTP of the rides after the ones imported may rise: their power metrics computed again
	if j.ridesImported && j.user.FTP == 0 {
		recomputePowerMetrics(App, user.ID)
	}
	activities := j.report.Count("activities")
	message := notifications.PushMessage{Title: "Import finished",
		Body: fmt.Sprintf("%d activities imported, %d already there, %d skipped", activities.Created,
			activities.Existing, activities.Skipped),
		URL: "/#/profile"}
	if _, err := db.NotifyUser(user.ID, models.NotifyImport, message.Encode()); err != nil {
		logger.Warn().Err(err).Msg("end of the import not notified")
	}
	logger.Info().Interface("counts", j.report.Counts).Msg("import of an export finished")
}

// progress saves the counts of the import; context.Canceled when it was cancelled meanwhile
func (j *accountImport) progress() error {
	saved, err := j.db.SaveAccountImportProgress(j.imp)
	if err == nil && !saved {
		return context.Canceled
	}
	return err
}

// importProfile applies the settings of profile.json, the dashboard layout and the columns of the activities
// table, when the user asked for them
func (j *accountImport) importProfile(context.Context) error {
	if !j.imp.WithProfile {
		return nil
	}
	var profile struct {
		userUpdate
		WeightObjective *float64                 `json:"weight_objective"`
		DashboardLayout []models.DashboardWidget `json:"dashboard_layout"`
		// absent from the exports made before them
		ActivitiesColumns []string `json:"activities_columns"`
	}
	found, err := j.export.decode("profile.json", &profile)
	if err != nil || !found {
		if err != nil {
			j.report.Skip("profile", "profile.json", err)
		}
		return nil
	}
	// never the address, nor the password: the account stays the user's
	update := profile.userUpdate
	update.Email, update.Password, update.OldPassword = nil, "", ""
	fields, err := update.fields(time.Now())
	if err != nil {
		j.report.Skip("profile", "profile.json", err)
		return nil
	}
	if err := j.db.UpdateUserFields(j.user.ID, fields); err != nil {
		return err
	}
	if maxHRChanged(j.user, update) {
		recomputeHrZones(j.App, j.user.ID)
	}
	if update.FTP != nil && *update.FTP != j.user.FTP {
		recomputePowerMetrics(j.App, j.user.ID)
	}
	if w := profile.WeightObjective; w != nil && *w >= 0 && *w <= maxWeight {
		if err := j.db.SetWeightObjective(j.user.ID, *w); err != nil {
			return err
		}
	}
	if profile.DashboardLayout != nil {
		if err := models.ValidateDashboardLayout(profile.DashboardLayout); err != nil {
			j.report.Skip("profile", "dashboard layout", err)
		} else if err := j.db.SetDashboardLayout(j.user.ID, profile.DashboardLayout); err != nil {
			return err
		}
	}
	if profile.ActivitiesColumns != nil {
		if err := models.ValidateActivitiesColumns(profile.ActivitiesColumns); err != nil {
			j.report.Skip("profile", "activities columns", err)
		} else if err := j.db.SetActivitiesColumns(j.user.ID, profile.ActivitiesColumns); err != nil {
			return err
		}
	}
	j.report.Count("profile").Created++
	// the activities are analysed with the new settings: the weight, the heart rate, the privacy zones
	j.user, err = j.db.GetUser(uint64(j.user.ID))
	return err
}

// importDevices creates the devices of the export the user doesn't have, with their name and their altitude
// correction, and merges the ones the export merged
func (j *accountImport) importDevices(context.Context) error {
	var devices []struct {
		Name               string               `json:"name"`
		Brand              string               `json:"brand"`
		Model              string               `json:"model"`
		Serial             string               `json:"serial_number"`
		Label              string               `json:"label"`
		AltitudeCorrection string               `json:"altitude_correction"`
		Aliases            []models.DeviceAlias `json:"aliases"`
	}
	if _, err := j.export.decode("devices.json", &devices); err != nil {
		j.report.Skip("devices", "devices.json", err)
		return nil
	}
	count := j.report.Count("devices")
	for _, d := range devices {
		existing, err := j.db.FindDevice(j.user.ID, d.Brand, d.Model, d.Serial)
		if err != nil {
			return err
		}
		if existing != nil {
			count.Existing++
			continue
		}
		if d.Name == "" {
			j.report.Skip("devices", d.Brand+" "+d.Model, errors.New("a device without name"))
			continue
		}
		id, err := j.db.FindOrCreateDevice(j.user.ID, types.Device{Brand: d.Brand, Model: d.Model, Serial: d.Serial},
			d.Name, d.AltitudeCorrection)
		if err != nil {
			return err
		}
		if label := strings.TrimSpace(d.Label); label != "" && utf8.RuneCountInString(label) <= models.MaxDeviceLabel {
			if err := j.db.UpdateDevice(j.user.ID, *id, &label, nil); err != nil {
				return err
			}
		}
		count.Created++
		for _, alias := range d.Aliases {
			// a device of the user stays as it is
			if found, err := j.db.FindDevice(j.user.ID, alias.Brand, alias.Model, alias.Serial); err != nil {
				return err
			} else if found != nil || alias.Name == "" {
				continue
			}
			aliasID, err := j.db.FindOrCreateDevice(j.user.ID, types.Device{Brand: alias.Brand, Model: alias.Model,
				Serial: alias.Serial}, alias.Name, models.AltitudeCorrectionNone)
			if err != nil {
				return err
			}
			if err := j.db.MergeDevices(j.user.ID, *aliasID, *id); err != nil && !errors.Is(err, models.ErrMergeDevice) {
				return err
			}
		}
	}
	return nil
}

// equipmentKey is what tells two equipments apart: the same name, brand and model are the same equipment
func equipmentKey(name, brand, model string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "\x00" + strings.ToLower(strings.TrimSpace(brand)) + "\x00" +
		strings.ToLower(strings.TrimSpace(model))
}

// importEquipments creates the equipments of the export the user doesn't have, with their picture,
// maintenance reminders and services, and the sports they are the default of when the user has none
func (j *accountImport) importEquipments(context.Context) error {
	var equipments []struct {
		ID             uuid.UUID `json:"id"`
		Name           string    `json:"name"`
		Brand          string    `json:"brand"`
		Model          string    `json:"model"`
		DateOfPurchase time.Time `json:"date_of_purchase"`
		InitialMileage int       `json:"initial_mileage"`
		Mileage        int       `json:"mileage"`
		Weight         int       `json:"weight"`
		IsCommute      bool      `json:"is_commute"`
		DefaultSports  []string  `json:"default_sports"`
		PictureFile    string    `json:"picture_file"`
		Reminders      []struct {
			ID uuid.UUID `json:"id"`
			reminderInput
		} `json:"reminders"`
		Services []struct {
			serviceInput
		} `json:"services"`
	}
	if _, err := j.export.decode("equipments.json", &equipments); err != nil {
		j.report.Skip("equipments", "equipments.json", err)
		return nil
	}
	current, err := j.db.GetEquipmentsStats(j.user.ID)
	if err != nil {
		return err
	}
	existing := map[string]uuid.UUID{}
	for _, e := range current {
		existing[equipmentKey(e.Name, e.Brand, e.EquipmentModel)] = e.ID
	}
	count := j.report.Count("equipments")
	for _, e := range equipments {
		key := equipmentKey(e.Name, e.Brand, e.Model)
		if id, ok := existing[key]; ok {
			j.equipments[e.ID] = id
			count.Existing++
			continue
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			j.report.Skip("equipments", e.ID.String(), errors.New("an equipment without name"))
			continue
		}
		equipment := models.Equipments{Name: name, Brand: e.Brand, EquipmentModel: e.Model,
			DateOfPurchase: e.DateOfPurchase, InitialMileage: e.InitialMileage, Mileage: e.Mileage, Weight: e.Weight,
			UserID: int(j.user.ID), DefaultSports: []string{}}
		// the commute equipment and the default ones of the user stay theirs
		if e.IsCommute {
			commute, err := j.db.GetCommuteEquipment(j.user.ID)
			if err != nil {
				return err
			}
			equipment.IsCommute = commute == nil
		}
		for _, sport := range e.DefaultSports {
			if !activityService.IsSportName(sport) {
				continue
			}
			if other, err := j.db.DefaultEquipment(j.user.ID, sport); err != nil {
				return err
			} else if other == nil {
				equipment.DefaultSports = append(equipment.DefaultSports, sport)
			}
		}
		if err := j.db.CreateEquipment(&equipment); err != nil {
			return err
		}
		j.equipments[e.ID] = equipment.ID
		existing[key] = equipment.ID
		count.Created++
		if e.PictureFile != "" {
			if err := j.importPicture(equipment.ID, e.PictureFile); err != nil {
				j.report.Skip("pictures", e.PictureFile, err)
			} else {
				j.report.Count("pictures").Created++
			}
		}
		for _, r := range e.Reminders {
			reminder := models.MaintenanceReminder{EquipmentID: equipment.ID}
			if err := r.reminderInput.apply(&reminder); err != nil {
				j.report.Skip("reminders", name+": "+r.Name, err)
				continue
			}
			if err := j.db.CreateReminder(&reminder); err != nil {
				return err
			}
			j.reminders[r.ID] = reminder.ID
			j.report.Count("reminders").Created++
		}
		for _, s := range e.Services {
			service := models.Services{EquipmentID: equipment.ID}
			ids := []uuid.UUID{}
			if s.ReminderIDs != nil {
				for _, id := range *s.ReminderIDs {
					if mapped, ok := j.reminders[id]; ok {
						ids = append(ids, mapped)
					}
				}
			}
			s.ReminderIDs = &ids
			if err := s.serviceInput.apply(&service); err != nil {
				j.report.Skip("services", name+": "+s.Description, err)
				continue
			}
			if err := j.db.CreateService(&service); err != nil {
				return err
			}
			j.report.Count("services").Created++
		}
	}
	return nil
}

// importPicture gives an equipment the picture of the export, processed as an upload is
func (j *accountImport) importPicture(equipmentID uuid.UUID, name string) error {
	f := j.export.files[name]
	if f == nil || !strings.HasPrefix(name, "pictures/") {
		return errors.New("the picture isn't in the export")
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	picture, err := pictures.Process(r)
	if err != nil {
		return err
	}
	stored, err := activityService.SaveEquipmentPicture(j.user.UUID, equipmentID, picture)
	if err != nil {
		return err
	}
	if err := j.db.SetEquipmentPicture(equipmentID, stored); err != nil {
		_ = activityService.RemoveEquipmentPicture(j.user.UUID, stored)
		return err
	}
	return nil
}

// importHealth imports the health measures: a measure at the date of one of the user completes it
func (j *accountImport) importHealth(context.Context) error {
	var measures []models.HealthData
	if _, err := j.export.decode("health.json", &measures); err != nil {
		j.report.Skip("health", "health.json", err)
		return nil
	}
	valid := measures[:0]
	for _, m := range measures {
		if err := m.Validate(); err != nil {
			j.report.Skip("health", m.Date.Format(time.DateOnly), err)
			continue
		}
		valid = append(valid, m)
	}
	count := j.report.Count("health")
	for start := 0; start < len(valid); start += healthImportChunk {
		result, err := j.db.ImportHealthDatas(j.user.ID, valid[start:min(start+healthImportChunk, len(valid))])
		if err != nil {
			return err
		}
		count.Created += result.Created
		count.Existing += result.Merged + result.Unchanged
	}
	return nil
}

// exportedActivity is what an import reads of an activity of the export: its file, and what the user set
type exportedActivity struct {
	ID          uuid.UUID  `json:"id"`
	Date        time.Time  `json:"date"`
	Sport       string     `json:"sport"`
	Title       string     `json:"title"`
	Filename    string     `json:"filename"`
	FilePath    string     `json:"file_path"`
	IsCommute   bool       `json:"is_commute"`
	EquipmentID *uuid.UUID `json:"equipment_id"`
	Visibility  int        `json:"visibility"`
	CanComments bool       `json:"can_comments"`
	TrimEnd     float64    `json:"trim_end"`
	RouteID     *uuid.UUID `json:"route_id"`
}

// importActivities imports the activities, the oldest first: each analysed from its file as an upload, then
// given back what the user set. An activity whose file is missing or can't be analysed is skipped.
func (j *accountImport) importActivities(ctx context.Context) error {
	count := j.report.Count("activities")
	for i, name := range j.export.activities {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.importActivity(ctx, name, count); err != nil {
			return err
		}
		j.imp.Done = i + 1
		if err := j.progress(); err != nil {
			return err
		}
	}
	return nil
}

// importActivity imports an activity of the export; an error stops the import, the activity skipped is in
// the report
func (j *accountImport) importActivity(ctx context.Context, name string, count *models.ImportCount) error {
	var a exportedActivity
	if _, err := j.export.decode(name, &a); err != nil {
		j.report.Skip("activities", name, err)
		return nil
	}
	if a.RouteID != nil {
		j.routeActivities[*a.RouteID] = append(j.routeActivities[*a.RouteID], a.ID)
	}
	// found without analysing it: the date and the sport of the export are the ones of its analysis
	if existing, err := j.db.FindDuplicateActivity(j.user.ID, a.Sport, a.Date); err != nil {
		return err
	} else if existing != nil {
		j.activities[a.ID] = *existing
		count.Existing++
		return nil
	}
	if !strings.HasPrefix(a.FilePath, "files/") || j.export.files[a.FilePath] == nil {
		j.report.Skip("activities", name, errors.New("its file isn't in the export"))
		return nil
	}
	tmp, err := activityService.CreateImportFile(j.user.UUID)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // fails harmlessly once the file is moved in place
	if err := j.export.extract(a.FilePath, tmp); err != nil {
		j.report.Skip("activities", name, err)
		return nil
	}
	filename := a.Filename
	if filename == "" {
		filename = path.Base(a.FilePath)
	}
	activity, err := activityService.ImportFile(ctx, j.db, j.user, tmp, filename, activityService.ImportOptions{
		Sport: a.Sport, Wait: true, Logger: j.App.Logger})
	var duplicate *activityService.DuplicateError
	switch {
	case errors.As(err, &duplicate):
		j.activities[a.ID] = duplicate.ExistingID
		count.Existing++
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		// the next activities may import: the report tells this one, a new import of the zip tries it again
		j.report.Skip("activities", name, err)
		return nil
	}
	j.activities[a.ID] = activity.ID
	j.created[activity.ID] = true
	count.Created++
	if activity.Sport == models.FTPSport {
		j.ridesImported = true
	}
	if err := j.restoreActivity(activity.ID, a); err != nil {
		j.report.Problem(name, fmt.Errorf("imported, but not as the user set it: %w", err))
	}
	return nil
}

// restoreActivity gives an activity just imported what the user set in the export: its trim, its title,
// equipment, commute, visibility
func (j *accountImport) restoreActivity(id uuid.UUID, a exportedActivity) error {
	activity, err := j.db.GetActivity(id)
	if err != nil {
		return err
	}
	if a.TrimEnd > 0 && !math.IsInf(a.TrimEnd, 0) {
		trimmed, err := activityService.TrimActivity(j.db, activity, a.TrimEnd, j.App.Logger)
		if err != nil {
			return fmt.Errorf("not trimmed: %w", err)
		}
		activity = trimmed
	}
	// the equipment of the export, when it was imported; none when it had none; the default one otherwise
	requested := activity.EquipmentID
	if a.EquipmentID == nil {
		requested = nil
	} else if mapped, ok := j.equipments[*a.EquipmentID]; ok {
		requested = &mapped
	}
	requested, err = activityService.EquipmentOnUpdate(j.db, *activity, a.IsCommute, requested)
	if err != nil {
		return err
	}
	if title := strings.TrimSpace(a.Title); title != "" && utf8.RuneCountInString(title) <= MaxTitleLength {
		activity.Title = title
	}
	if a.Visibility >= 0 && a.Visibility <= 2 {
		activity.Visibility = a.Visibility
	}
	activity.CanComments = a.CanComments
	activity.IsCommute = a.IsCommute
	if activity.IsCommute && activity.Co2 == 0 {
		activity.Co2 = activityService.Commute(*activity)
	}
	activity.EquipmentID = requested
	// the preloaded association would override EquipmentID when saving
	activity.Equipment = models.Equipments{}
	return j.db.UpdateActivity(activity)
}

// importRoutes makes the routes of the commutes of the export commute routes again: the routes are found
// again by the analysis, the ones of the activities this import created
func (j *accountImport) importRoutes(context.Context) error {
	var routes []struct {
		ID        uuid.UUID `json:"id"`
		IsCommute bool      `json:"is_commute"`
	}
	if _, err := j.export.decode("routes.json", &routes); err != nil {
		j.report.Skip("commute routes", "routes.json", err)
		return nil
	}
	done := map[uuid.UUID]bool{}
	for _, r := range routes {
		if !r.IsCommute {
			continue
		}
		for _, old := range j.routeActivities[r.ID] {
			id, ok := j.activities[old]
			if !ok || !j.created[id] {
				continue
			}
			activity, err := j.db.GetActivity(id)
			if err != nil {
				return err
			}
			if activity.RouteID == nil || done[*activity.RouteID] {
				continue
			}
			route, err := j.db.GetRoute(*activity.RouteID)
			if err != nil {
				return err
			}
			done[route.ID] = true
			if route.IsCommute {
				j.report.Count("commute routes").Existing++
				continue
			}
			if _, err := j.db.SetRouteCommute(route.ID, true); err != nil {
				return err
			}
			j.report.Count("commute routes").Created++
		}
	}
	return nil
}

// importSegments creates the segments of the export the user doesn't have, and finds the passes of their
// activities on them
func (j *accountImport) importSegments(context.Context) error {
	var segments []struct {
		ID               uuid.UUID           `json:"id"`
		Name             string              `json:"name"`
		Sport            string              `json:"sport"`
		Points           types.GpsPointArray `json:"points"`
		ElevationGain    float64             `json:"elevation_gain"`
		SourceActivityID *uuid.UUID          `json:"source_activity_id"`
	}
	if _, err := j.export.decode("segments.json", &segments); err != nil {
		j.report.Skip("segments", "segments.json", err)
		return nil
	}
	var current []models.Segment
	if err := j.db.Where("user_id = ?", j.user.ID).Find(&current).Error; err != nil {
		return err
	}
	existing := map[string]bool{}
	key := func(name, sport string, distance float64) string {
		return fmt.Sprintf("%s\x00%s\x00%.0f", strings.ToLower(strings.TrimSpace(name)), sport, distance)
	}
	for _, s := range current {
		existing[key(s.Name, s.Sport, s.Distance)] = true
	}
	count := j.report.Count("segments")
	for _, s := range segments {
		segment, err := activityService.SegmentFromPoints(j.user.ID, s.Name, s.Sport, s.Points, s.ElevationGain)
		if err != nil {
			j.report.Skip("segments", s.Name, err)
			continue
		}
		if existing[key(segment.Name, segment.Sport, segment.Distance)] {
			count.Existing++
			continue
		}
		if s.SourceActivityID != nil {
			if id, ok := j.activities[*s.SourceActivityID]; ok {
				segment.SourceActivityID = &id
			}
		}
		if err := j.db.CreateSegment(&segment); err != nil {
			return err
		}
		existing[key(segment.Name, segment.Sport, segment.Distance)] = true
		count.Created++
		if _, err := activityService.MatchSegmentActivities(j.db, segment, activityService.MatchCreate); err != nil {
			return err
		}
	}
	return nil
}

// importGoals creates the goals of the export the user doesn't have: the same name, metric and period
func (j *accountImport) importGoals(context.Context) error {
	var goals []goalInput
	if _, err := j.export.decode("goals.json", &goals); err != nil {
		j.report.Skip("goals", "goals.json", err)
		return nil
	}
	var current []models.Goal
	if err := j.db.Where("user_id = ?", j.user.ID).Find(&current).Error; err != nil {
		return err
	}
	key := func(name, metric, period string) string {
		return strings.ToLower(strings.TrimSpace(name)) + "\x00" + metric + "\x00" + period
	}
	existing := map[string]bool{}
	for _, g := range current {
		existing[key(g.Name, g.Metric, g.Period)] = true
	}
	now := time.Now().In(activityService.UserLocation(j.user.Timezone))
	count := j.report.Count("goals")
	for _, input := range goals {
		if existing[key(input.Name, input.Metric, input.Period)] {
			count.Existing++
			continue
		}
		goal := models.Goal{UserID: j.user.ID}
		if err := input.apply(&goal); err != nil {
			j.report.Skip("goals", input.Name, err)
			continue
		}
		if err := j.db.SaveGoal(&goal, now); err != nil {
			return err
		}
		existing[key(input.Name, input.Metric, input.Period)] = true
		count.Created++
	}
	return nil
}

// importWorkplaces creates the workplaces of the export the user doesn't have: the same name and center. An
// export without workplaces.json (older) has none.
func (j *accountImport) importWorkplaces(context.Context) error {
	var workplaces []workplaceInput
	if found, err := j.export.decode("workplaces.json", &workplaces); err != nil {
		j.report.Skip("workplaces", "workplaces.json", err)
		return nil
	} else if !found {
		return nil
	}
	current, err := j.db.GetWorkplaces(j.user.ID)
	if err != nil {
		return err
	}
	key := func(name string, lat, lon float64) string {
		return fmt.Sprintf("%s\x00%.6f\x00%.6f", strings.ToLower(strings.TrimSpace(name)), lat, lon)
	}
	existing := map[string]bool{}
	for _, w := range current {
		existing[key(w.Name, w.Lat, w.Lon)] = true
	}
	count := j.report.Count("workplaces")
	for _, input := range workplaces {
		if existing[key(input.Name, input.Lat, input.Lon)] {
			count.Existing++
			continue
		}
		if len(existing) >= maxWorkplaces {
			j.report.Skip("workplaces", input.Name, fmt.Errorf("%d workplaces at most", maxWorkplaces))
			continue
		}
		workplace := models.Workplace{UserID: j.user.ID}
		if err := input.apply(&workplace); err != nil {
			j.report.Skip("workplaces", input.Name, err)
			continue
		}
		if err := j.db.SaveWorkplace(&workplace); err != nil {
			return err
		}
		existing[key(input.Name, input.Lat, input.Lon)] = true
		count.Created++
	}
	return nil
}

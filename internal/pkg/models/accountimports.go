package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The states of an import of an account
const (
	ImportPending   = "pending"   // the zip is stored, the import not started
	ImportRunning   = "running"   // resumed at the next start when the server stops meanwhile
	ImportDone      = "done"      // see its report, some items may have been skipped
	ImportFailed    = "failed"    // see its error: what was imported before stays
	ImportCancelled = "cancelled" // by the user: what was imported before stays
)

// ErrImportRunning is a new import of a user whose import isn't finished
var ErrImportRunning = errors.New("an import of your data is already running")

// AccountImport is the import of the zip of an export into the account of a user (see the import of the
// apicontrollers). Everything already in the account is kept: what the export has again is counted as
// existing, not imported twice.
type AccountImport struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at"`
	UserID     uint       `gorm:"not null" json:"-"`
	Status     string     `gorm:"not null" json:"status"`
	// ZipPath is the zip, removed once the import is finished
	ZipPath     string `gorm:"not null;default:''" json:"-"`
	WithProfile bool   `gorm:"not null;default:false" json:"with_profile"`
	// Format is the one of the export, see activityService.ExportFormat
	Format int `gorm:"not null;default:0" json:"format"`
	// Total is the number of activities of the export, Done the ones handled (imported, existing or skipped)
	Total  int           `gorm:"not null;default:0" json:"total"`
	Done   int           `gorm:"not null;default:0" json:"done"`
	Report *ImportReport `gorm:"type:jsonb;serializer:json" json:"report"`
	Error  string        `gorm:"not null;default:''" json:"error"`
}

// ImportReport is what an import did, by kind of item ("activities", "equipments"...)
type ImportReport struct {
	Counts map[string]*ImportCount `json:"counts"`
	// Problems are the items skipped and why, the first MaxImportProblems
	Problems []ImportProblem `json:"problems"`
	// MoreProblems is the number of problems beyond MaxImportProblems
	MoreProblems int `json:"more_problems"`
}

// ImportCount counts the items of a kind: created, already in the account, or skipped (see Problems)
type ImportCount struct {
	Created  int `json:"created"`
	Existing int `json:"existing"`
	Skipped  int `json:"skipped"`
}

// ImportProblem is an item an import skipped: the file of the export, and why
type ImportProblem struct {
	Item  string `json:"item"`
	Error string `json:"error"`
}

// MaxImportProblems is the number of problems a report keeps
const MaxImportProblems = 200

// Count returns the counts of a kind of item, created at its first use
func (r *ImportReport) Count(kind string) *ImportCount {
	if r.Counts == nil {
		r.Counts = map[string]*ImportCount{}
	}
	if r.Counts[kind] == nil {
		r.Counts[kind] = &ImportCount{}
	}
	return r.Counts[kind]
}

// Skip counts an item of a kind skipped, and why
func (r *ImportReport) Skip(kind, item string, err error) {
	r.Count(kind).Skipped++
	r.Problem(item, err)
}

// Problem tells what went wrong with an item, imported or not
func (r *ImportReport) Problem(item string, err error) {
	if len(r.Problems) >= MaxImportProblems {
		r.MoreProblems++
		return
	}
	r.Problems = append(r.Problems, ImportProblem{Item: item, Error: err.Error()})
}

// CreateAccountImport saves a new import, pending; ErrImportRunning when the user has one not finished
func (db *DB) CreateAccountImport(i *AccountImport) error {
	i.Status = ImportPending
	err := db.Transaction(func(tx *gorm.DB) error {
		// the user locked: two uploads at once can't both find no import running
		if err := tx.Model(&Users{}).Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
			Where("id = ?", i.UserID).Take(&Users{}).Error; err != nil {
			return err
		}
		var running int64
		if err := tx.Model(&AccountImport{}).Where("user_id = ? AND status IN ?", i.UserID,
			[]string{ImportPending, ImportRunning}).Count(&running).Error; err != nil {
			return err
		}
		if running > 0 {
			return ErrImportRunning
		}
		return tx.Create(i).Error
	})
	return err
}

// GetLatestAccountImport returns the latest import of a user, gorm.ErrRecordNotFound without
func (db *DB) GetLatestAccountImport(userID uint) (AccountImport, error) {
	var i AccountImport
	err := db.Where("user_id = ?", userID).Order("created_at DESC").First(&i).Error
	return i, err
}

// GetAccountImport returns an import, gorm.ErrRecordNotFound without
func (db *DB) GetAccountImport(id uuid.UUID) (AccountImport, error) {
	var i AccountImport
	err := db.Where("id = ?", id).First(&i).Error
	return i, err
}

// SaveAccountImportProgress saves the state, the counts and the report of an import, unless it was finished
// meanwhile (cancelled): false then
func (db *DB) SaveAccountImportProgress(i *AccountImport) (bool, error) {
	result := db.Model(&AccountImport{}).Where("id = ? AND status IN ?", i.ID, []string{ImportPending, ImportRunning}).
		Select("status", "format", "total", "done", "report", "updated_at").Updates(i)
	return result.RowsAffected > 0, result.Error
}

// FinishAccountImport ends an import with status (done, failed or cancelled), unless it is finished already:
// false then
func (db *DB) FinishAccountImport(i *AccountImport, status, failure string) (bool, error) {
	now := time.Now()
	i.Status, i.Error, i.FinishedAt = status, failure, &now
	result := db.Model(&AccountImport{}).Where("id = ? AND status IN ?", i.ID, []string{ImportPending, ImportRunning}).
		Select("status", "error", "finished_at", "done", "report", "updated_at").Updates(i)
	return result.RowsAffected > 0, result.Error
}

// GetUnfinishedAccountImports returns the imports a stop of the server interrupted, the oldest first
func (db *DB) GetUnfinishedAccountImports() ([]AccountImport, error) {
	var imports []AccountImport
	err := db.Where("status IN ?", []string{ImportPending, ImportRunning}).Order("created_at").Find(&imports).Error
	return imports, err
}

// FindDevice returns a device of a user by its unit, nil when the user has no such device
func (db *DB) FindDevice(userID uint, brand, model, serial string) (*Devices, error) {
	var found []Devices
	err := db.Where(unitOf, userID, brand, model, serial).Limit(1).Find(&found).Error
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return &found[0], nil
}

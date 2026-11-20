package models

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"gorm.io/gorm"
)

// The schema is versioned with goose: the version 1 is the baseline, the schema GORM's AutoMigrate created
// until then; each later change is a file migrations/NNNNN_what.sql (-- +goose Up, and -- +goose Down when
// it can be undone), its version the next number. The applied versions are in the goose_db_version table.

//go:embed schema/baseline.sql
var baselineSQL string

//go:embed migrations
var migrationFiles embed.FS

// ErrDatabaseNewer is a database migrated by a newer version of the backend: this one doesn't know its
// schema, it must not run on it (a rollback needs the database backup of before the upgrade)
var ErrDatabaseNewer = errors.New("the database schema is newer than this version of the backend")

// ErrLegacyTooOld is a database never versioned, older than the migrations after the baseline
var ErrLegacyTooOld = errors.New("the database predates the versioned migrations and this version has migrations " +
	"after the baseline: upgrade first to the last version with only the baseline migration, it adopts the database")

// models are the tables of the application: the schema must have their columns (SchemaDrift)
func models() []interface{} {
	return []interface{}{&Users{}, &Equipments{}, &Devices{}, &Services{}, &Activity{}, &Route{}, &HealthData{},
		&Validations{}, &Keys{}, &Notifications{}, &DeviceTokens{}, &RevokedSessions{}}
}

// CreateOrMigrate brings the schema of the database to the version of the backend, applying the pending
// migrations, one server at a time (a Postgres advisory lock). A database created by the versions before
// the migrations (AutoMigrate) is adopted by the baseline. ErrDatabaseNewer for a database migrated by a
// newer backend.
func CreateOrMigrate(db *DB) error {
	ctx := context.Background()
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	provider, err := newProvider(sqlDB)
	if err != nil {
		return err
	}
	// never provider.Close(): it closes the connection pool of GORM
	sources := provider.ListSources()
	latest := sources[len(sources)-1].Version
	if legacy, err := isLegacy(ctx, sqlDB); err != nil {
		return err
	} else if legacy && len(sources) > 1 {
		return ErrLegacyTooOld
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	if current > latest {
		return fmt.Errorf("%w: database at version %d, this backend knows up to %d", ErrDatabaseNewer, current, latest)
	}
	_, err = provider.Up(ctx)
	return err
}

// SchemaVersion returns the version of the schema of the database, and the latest one the backend knows
func SchemaVersion(db *DB) (current, latest int64, err error) {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return 0, 0, err
	}
	provider, err := newProvider(sqlDB)
	if err != nil {
		return 0, 0, err
	}
	sources := provider.ListSources()
	current, err = provider.GetDBVersion(context.Background())
	return current, sources[len(sources)-1].Version, err
}

func newProvider(sqlDB *sql.DB) (*goose.Provider, error) {
	files, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, sqlDB, files,
		goose.WithGoMigrations(goose.NewGoMigration(1, &goose.GoFunc{RunTx: baseline}, nil)),
		goose.WithSessionLocker(locker))
}

// isLegacy tells whether the database was created by AutoMigrate, before the versioned migrations: it has
// the tables of the application but not the versions table
func isLegacy(ctx context.Context, sqlDB *sql.DB) (bool, error) {
	var users, versions bool
	err := sqlDB.QueryRowContext(ctx, `SELECT to_regclass('users') IS NOT NULL, to_regclass('goose_db_version') IS NOT NULL`).
		Scan(&users, &versions)
	return users && !versions, err
}

// baseline is the migration 1: it creates the schema in a new database. The databases created by AutoMigrate,
// before the versioned migrations, were adopted by the releases with only this migration; this one refuses
// them (ErrLegacyTooOld) before migrating.
func baseline(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, baselineSQL)
	return err
}

// SchemaDrift returns the tables and columns the models have and the database hasn't: a change of a model
// without its migration. Empty when they match.
func SchemaDrift(db *DB) ([]string, error) {
	var missing []string
	for _, model := range models() {
		stmt := &gorm.Statement{DB: &db.DB}
		if err := stmt.Parse(model); err != nil {
			return nil, err
		}
		table := stmt.Schema.Table
		if !db.Migrator().HasTable(table) {
			missing = append(missing, "table "+table)
			continue
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName != "" && !db.Migrator().HasColumn(model, field.DBName) {
				missing = append(missing, "column "+table+"."+field.DBName)
			}
		}
	}
	return missing, nil
}

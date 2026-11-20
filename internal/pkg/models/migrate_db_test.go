package models

import (
	"errors"
	"fmt"
	"testing"
)

// resetSchema empties the test database, tables and versions: healthTestDB refuses a database whose name
// doesn't contain "test". The database is left migrated by the tests, for the next ones.
func resetSchema(t *testing.T) *DB {
	t.Helper()
	db := healthTestDB(t)
	if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// columns returns the columns of the tables of the application, "table.column type nullable default"
func columns(t *testing.T, db *DB) map[string]bool {
	t.Helper()
	var rows []struct {
		TableName     string
		ColumnName    string
		DataType      string
		IsNullable    string
		ColumnDefault *string
	}
	err := db.Raw(`SELECT table_name, column_name, data_type, is_nullable, column_default
		FROM information_schema.columns WHERE table_schema = 'public' AND table_name <> 'goose_db_version'`).
		Scan(&rows).Error
	if err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, r := range rows {
		def := ""
		if r.ColumnDefault != nil {
			def = *r.ColumnDefault
		}
		set[fmt.Sprintf("%s.%s %s %s %s", r.TableName, r.ColumnName, r.DataType, r.IsNullable, def)] = true
	}
	return set
}

func checkVersion(t *testing.T, db *DB, want int64) {
	t.Helper()
	current, latest, err := SchemaVersion(db)
	if err != nil || current != want || latest < want {
		t.Errorf("schema version %d of %d (%v), want %d", current, latest, err, want)
	}
}

// TestSchemaMatchesModels fails on a field of a model without its column: a migration is missing
func TestSchemaMatchesModels(t *testing.T) {
	db := resetSchema(t)
	if err := CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	_, latest, _ := SchemaVersion(db)
	checkVersion(t, db, latest)
	missing, err := SchemaDrift(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range missing {
		t.Errorf("%s: add its migration in migrations/", m)
	}
	// again: nothing pending
	if err := CreateOrMigrate(db); err != nil {
		t.Errorf("second run: %v", err)
	}
}

// a database of before the versioned migrations (AutoMigrate, no versions table) is refused once there are
// migrations after the baseline, untouched: the baseline adopts it as the schema of its time, the later
// migrations would fail on it. The previous release, with only the baseline, adopts it first.
func TestLegacyDatabaseRefused(t *testing.T) {
	db := resetSchema(t)
	// leaves the database usable by the next tests
	// (not resetSchema: it migrates first, which the legacy database refuses)
	defer func() {
		if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public").Error; err != nil {
			t.Fatal(err)
		}
		if err := CreateOrMigrate(db); err != nil {
			t.Fatal(err)
		}
	}()
	if err := legacyAutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO users (username, email) VALUES ('legacy', 'legacy@test.local')").Error; err != nil {
		t.Fatal(err)
	}
	if err := CreateOrMigrate(db); !errors.Is(err, ErrLegacyTooOld) {
		t.Fatalf("a legacy database: %v, want ErrLegacyTooOld", err)
	}
	var count int64
	if err := db.Table("users").Where("username = 'legacy'").Count(&count).Error; err != nil || count != 1 {
		t.Errorf("the user of the legacy database: %d (%v)", count, err)
	}
	if db.Migrator().HasTable("goose_db_version") {
		t.Error("the legacy database got a versions table: it would pass for adopted")
	}
}

// TestMigrateNewerDatabase refuses a database migrated by a newer backend
func TestMigrateNewerDatabase(t *testing.T) {
	db := resetSchema(t)
	if err := CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO goose_db_version (version_id, is_applied) VALUES (99, true)").Error; err != nil {
		t.Fatal(err)
	}
	if err := CreateOrMigrate(db); !errors.Is(err, ErrDatabaseNewer) {
		t.Errorf("a database at version 99: %v, want ErrDatabaseNewer", err)
	}
	// leaves the database usable by the next tests
	if err := db.Exec("DELETE FROM goose_db_version WHERE version_id = 99").Error; err != nil {
		t.Fatal(err)
	}
}

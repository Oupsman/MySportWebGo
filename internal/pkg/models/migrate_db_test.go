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

// TestMigrateLegacyDatabase adopts a database created by AutoMigrate before the migrations, even an older
// one: its data is kept, it gets the schema of a new database
func TestMigrateLegacyDatabase(t *testing.T) {
	db := resetSchema(t)
	if err := db.Exec(baselineSQL).Error; err != nil {
		t.Fatal(err)
	}
	baseline := columns(t, db)

	db = resetSchema(t)
	if err := legacyAutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if legacy := columns(t, db); fmt.Sprint(legacy) != fmt.Sprint(baseline) {
		for c := range baseline {
			if !legacy[c] {
				t.Errorf("baseline.sql has %s, AutoMigrate hasn't", c)
			}
		}
		for c := range legacy {
			if !baseline[c] {
				t.Errorf("AutoMigrate has %s, baseline.sql hasn't", c)
			}
		}
	}
	// an older database: a column added since, one dropped since, data
	for _, statement := range []string{
		"ALTER TABLE activities DROP COLUMN best_efforts",
		"ALTER TABLE activities ADD COLUMN stances bytea",
		"INSERT INTO users (username, email) VALUES ('legacy', 'legacy@test.local')",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	checkVersion(t, db, 1)
	if migrated := columns(t, db); fmt.Sprint(migrated) != fmt.Sprint(baseline) {
		t.Errorf("the adopted database differs from a new one:\n%v\nwant\n%v", migrated, baseline)
	}
	var count int64
	if err := db.Table("users").Where("username = 'legacy'").Count(&count).Error; err != nil || count != 1 {
		t.Errorf("the user of the legacy database: %d (%v)", count, err)
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

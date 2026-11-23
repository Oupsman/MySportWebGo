package models

import (
	"os"
	"regexp"
	"testing"
)

// the role of the application owns the database and its objects, migrates and uses it, and can't do what a
// superuser does; set up again, it takes back what a superuser created since
func TestEnsureAppRole(t *testing.T) {
	db := healthTestDB(t)
	dsn := os.Getenv("MYSPORTWEB_TEST_DSN")
	if super, err := db.IsSuperuser(); err != nil || !super {
		t.Skipf("the test database user isn't a superuser (%v)", err)
	}
	const role, password = "mysportweb_test_app", "app role password"
	if err := db.EnsureAppRole(role, password); err != nil {
		t.Fatal(err)
	}
	// once more: nothing to change, no error
	if err := db.EnsureAppRole(role, password); err != nil {
		t.Fatalf("second time: %v", err)
	}
	t.Cleanup(func() {
		// back to the superuser, for the other tests (it can do anything anyway)
		db.Exec("SELECT set_config('mysportweb.app_role', current_user, false)")
		db.Exec(giveToAppRole)
	})

	appDSN := regexp.MustCompile(`user=\S+`).ReplaceAllString(dsn, "user="+role)
	appDSN = regexp.MustCompile(`password=('[^']*'|\S+)`).ReplaceAllString(appDSN, "password='"+password+"'")
	app, err := ConnectToDB("postgres", appDSN)
	if err != nil {
		t.Fatalf("connecting as the role of the application: %v", err)
	}
	if sqlDB, err := app.DB.DB(); err == nil {
		defer sqlDB.Close()
	}
	if super, err := app.IsSuperuser(); err != nil || super {
		t.Fatalf("role of the application superuser: %v %v", super, err)
	}
	// it migrates (nothing to do), changes the schema, writes and reads
	if err := CreateOrMigrate(app); err != nil {
		t.Fatalf("migrating as the role of the application: %v", err)
	}
	for _, statement := range []string{
		"ALTER TABLE users ADD COLUMN role_test integer",
		"ALTER TABLE users DROP COLUMN role_test",
		"INSERT INTO users (username, email, created_at, updated_at) VALUES ('roletest', 'roletest@test.local', now(), now())",
		"DELETE FROM users WHERE username = 'roletest'",
	} {
		if err := app.Exec(statement).Error; err != nil {
			t.Errorf("%s: %v", statement, err)
		}
	}
	// what a superuser does, refused
	for _, statement := range []string{
		"SELECT pg_read_file('/etc/passwd')",
		"COPY (SELECT 1) TO PROGRAM 'true'",
		"CREATE ROLE intruder SUPERUSER",
	} {
		if err := app.Exec(statement).Error; err == nil {
			t.Errorf("allowed to the role of the application: %s", statement)
		}
	}

	// a table created by the superuser (a restore) goes to the role at the next start
	if err := db.Exec("CREATE TABLE IF NOT EXISTS restored_by_superuser (id serial PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE IF EXISTS restored_by_superuser")
	if err := db.EnsureAppRole(role, password); err != nil {
		t.Fatal(err)
	}
	var owner string
	db.Raw("SELECT tableowner FROM pg_tables WHERE tablename = 'restored_by_superuser'").Scan(&owner)
	if owner != role {
		t.Errorf("restored table owned by %q", owner)
	}
	if err := app.Exec("INSERT INTO restored_by_superuser DEFAULT VALUES").Error; err != nil {
		t.Errorf("writing the restored table and its sequence: %v", err)
	}
	var others int64
	db.Raw(`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'v', 'm', 'S') AND pg_get_userbyid(c.relowner) <> ?`, role).Scan(&others)
	if others != 0 {
		t.Errorf("%d objects of public not owned by the role of the application", others)
	}
}

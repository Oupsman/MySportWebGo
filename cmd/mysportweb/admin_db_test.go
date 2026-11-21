package main

import (
	"MySportWeb/internal/pkg/models"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestAdminCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{nil, {"promote"}, {"delete", "alice"}, {"promote", "alice", "bob"}} {
		if code := adminCommand(args, &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit %d, want the usage", args, code)
		}
	}

	dsn := os.Getenv("MYSPORTWEB_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSPORTWEB_TEST_DSN is not set")
	}
	db, err := models.ConnectToDB("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil || !strings.Contains(name, "test") {
		t.Fatalf("refusing to empty database %q (%v), its name must contain \"test\"", name, err)
	}
	if err := models.CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("TRUNCATE users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Users{Username: "alice", Email: "alice@test.local", Role: models.RoleUser}).Error; err != nil {
		t.Fatal(err)
	}
	roleOf := func() string {
		var user models.Users
		db.Where("username = ?", "alice").First(&user)
		return user.Role
	}

	if code := runAdmin(db, []string{"promote", "alice"}, &stdout, &stderr); code != 0 || roleOf() != models.RoleAdmin {
		t.Errorf("promote: exit %d, role %s, %s", code, roleOf(), stderr.String())
	}
	stdout.Reset()
	if code := runAdmin(db, []string{"demote", "alice"}, &stdout, &stderr); code != 0 || roleOf() != models.RoleUser ||
		!strings.Contains(stdout.String(), "no admin") {
		t.Errorf("demote: exit %d, role %s, %q", code, roleOf(), stdout.String())
	}
	// case sensitive, as the logins
	if code := runAdmin(db, []string{"promote", "Alice"}, &stdout, &stderr); code != 1 || roleOf() != models.RoleUser {
		t.Errorf("unknown user: exit %d", code)
	}
}

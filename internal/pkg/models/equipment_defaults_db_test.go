package models

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEquipmentDefaultSports(t *testing.T) {
	db := healthTestDB(t)
	if err := db.Exec("TRUNCATE equipment_defaults, services, activities, equipments CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := Users{Username: "gear", Email: "gear@test.local"}
	other := Users{Username: "other", Email: "other@test.local"}
	for _, u := range []*Users{&user, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	bike := Equipments{Name: "bike", UserID: int(user.ID), DefaultSports: []string{"cycling"}}
	shoes := Equipments{Name: "shoes", UserID: int(user.ID), DefaultSports: []string{"running", "hiking", "running"}}
	theirs := Equipments{Name: "theirs", UserID: int(other.ID), DefaultSports: []string{"running"}}
	for _, e := range []*Equipments{&bike, &shoes, &theirs} {
		if err := db.CreateEquipment(e); err != nil {
			t.Fatal(err)
		}
	}
	defaultOf := func(userID uint, sport string) uuid.UUID {
		t.Helper()
		e, err := db.DefaultEquipment(userID, sport)
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			return uuid.Nil
		}
		return e.ID
	}
	sportsOf := func(e Equipments) []string {
		t.Helper()
		got, err := db.GetEquipment(e.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.DefaultSports
	}
	if defaultOf(user.ID, "cycling") != bike.ID || defaultOf(user.ID, "running") != shoes.ID ||
		defaultOf(user.ID, "hiking") != shoes.ID || defaultOf(other.ID, "running") != theirs.ID {
		t.Fatal("defaults after creation")
	}
	if defaultOf(user.ID, "swimming") != uuid.Nil {
		t.Error("a default for a sport nobody claimed")
	}
	if got := sportsOf(shoes); !slices.Equal(got, []string{"hiking", "running"}) {
		t.Errorf("shoes: %v, want sorted and unique", got)
	}

	// claiming a sport takes it from the other equipment of the user only
	bike.DefaultSports = []string{"cycling", "hiking"}
	if err := db.UpdateEquipment(&bike); err != nil {
		t.Fatal(err)
	}
	if defaultOf(user.ID, "hiking") != bike.ID || !slices.Equal(sportsOf(shoes), []string{"running"}) {
		t.Errorf("hiking not moved to the bike: shoes %v", sportsOf(shoes))
	}
	if defaultOf(other.ID, "running") != theirs.ID {
		t.Error("another user's default changed")
	}

	// nil keeps them, empty removes them
	bike.DefaultSports = nil
	bike.Name = "road bike"
	if err := db.UpdateEquipment(&bike); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sportsOf(bike), []string{"cycling", "hiking"}) {
		t.Errorf("nil changed the sports: %v", sportsOf(bike))
	}
	bike.DefaultSports = []string{}
	if err := db.UpdateEquipment(&bike); err != nil {
		t.Fatal(err)
	}
	if got := sportsOf(bike); got == nil || len(got) != 0 || defaultOf(user.ID, "cycling") != uuid.Nil {
		t.Errorf("empty kept the sports: %v", got)
	}

	// the list: the defaults first, with their sports; the default ones go with a deleted equipment
	stats, err := db.GetEquipmentsStats(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].ID != shoes.ID || !slices.Equal(stats[0].DefaultSports, []string{"running"}) ||
		stats[1].DefaultSports == nil {
		t.Errorf("list: %+v", stats)
	}
	if err := db.DeleteEquipment(shoes.ID); err != nil {
		t.Fatal(err)
	}
	if defaultOf(user.ID, "running") != uuid.Nil {
		t.Error("the default of a deleted equipment")
	}
}

// TestMigrateEquipmentDefaults: the default equipment of a user becomes the one of its most used sport
func TestMigrateEquipmentDefaults(t *testing.T) {
	db := resetSchema(t)
	sqlDB, err := db.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newProvider(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := provider.UpTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users (id, username, email) VALUES (1, 'runner', 'r@test.local'), " +
		"(2, 'newbie', 'n@test.local'), (3, 'twice', 't@test.local'), (4, 'none', 'x@test.local')")
	shoes, spare, bike, older, newer, plain := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	exec("INSERT INTO equipments (id, user_id, name, is_default, updated_at) VALUES "+
		"(?, 1, 'shoes', true, ?), (?, 1, 'spare', false, ?), (?, 2, 'bike', true, ?), "+
		"(?, 3, 'older', true, ?), (?, 3, 'newer', true, ?), (?, 4, 'plain', false, ?)",
		shoes, day, spare, day, bike, day, older, day, newer, day.AddDate(0, 0, 1), plain, day)
	for i, a := range []struct {
		equipment uuid.UUID
		sport     string
	}{{shoes, "running"}, {shoes, "running"}, {shoes, "hiking"}, {spare, "cycling"}} {
		exec("INSERT INTO activities (id, user_id, equipment_id, sport, date) VALUES (?, 1, ?, ?, ?)",
			uuid.New(), a.equipment, a.sport, day.AddDate(0, 0, i))
	}
	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatal(err)
	}
	var rows []EquipmentDefault
	if err := db.Order("user_id, sport").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// the most used sport; cycling without activity; the latest updated of two defaults
	want := []EquipmentDefault{{1, "running", shoes}, {2, "cycling", bike}, {3, "cycling", newer}}
	if !slices.Equal(rows, want) {
		t.Errorf("defaults %v, want %v", rows, want)
	}

	if _, err := provider.DownTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	var defaults []uuid.UUID
	if err := db.Raw("SELECT id FROM equipments WHERE is_default ORDER BY name").Scan(&defaults).Error; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(defaults, []uuid.UUID{bike, newer, shoes}) {
		t.Errorf("after down: %v", defaults)
	}
	// leaves the database usable by the next tests
	if err := CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
}

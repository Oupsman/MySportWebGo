package models

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// the migration 13 makes the maintenance interval of an equipment its "Service" reminder, with its services
// and its notification state; its Down brings the interval back
func TestMigrateMaintenanceReminders(t *testing.T) {
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
	// leaves the database migrated for the next tests
	defer func() {
		if _, err := provider.Up(ctx); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := provider.UpTo(ctx, 12); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users (id, username, email) VALUES (1, 'gear', 'gear@test.local')")
	bike, shoes, serviced := uuid.New(), uuid.New(), time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	exec("INSERT INTO equipments (id, name, user_id, maintenance_interval, service_alerted, service_alerted_for) VALUES (?, 'Gravel', 1, 3000, true, ?)", bike, serviced)
	exec("INSERT INTO equipments (id, name, user_id, maintenance_interval) VALUES (?, 'Shoes', 1, 0)", shoes)
	exec("INSERT INTO services (equipment_id, date, description) VALUES (?, ?, 'chain'), (?, ?, 'cables'), (?, ?, 'laces')",
		bike, serviced.AddDate(0, -2, 0), bike, serviced, shoes, serviced)

	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	var reminders []MaintenanceReminder
	if err := db.Find(&reminders).Error; err != nil {
		t.Fatal(err)
	}
	if len(reminders) != 1 {
		t.Fatalf("%d reminders, want the one of the interval", len(reminders))
	}
	r := reminders[0]
	if r.EquipmentID != bike || r.Name != "Service" || r.EveryKm != 3000 || r.EveryCount != 0 || !r.Notified ||
		r.NotifiedFor == nil || !r.NotifiedFor.Equal(serviced) || r.StartsAt.Year() != 1970 {
		t.Errorf("reminder %+v", r)
	}
	var links int64
	db.Model(&ServiceReminder{}).Where("reminder_id = ?", r.ID).Count(&links)
	if links != 2 {
		t.Errorf("%d services of the reminder, want those of the bike", links)
	}
	// notified for its cycle: the upgrade doesn't notify it again
	s, err := db.GetReminderStatus(bike, r.ID)
	if err != nil || s.LastDone == nil || !s.LastDone.Equal(serviced) || s.Notifiable {
		t.Errorf("status %+v (%v)", s, err)
	}
	if columns(t, db)["equipments.maintenance_interval bigint YES "] {
		t.Error("maintenance_interval is still there")
	}

	if _, err := provider.DownTo(ctx, 12); err != nil {
		t.Fatal(err)
	}
	var back struct {
		MaintenanceInterval int
		ServiceAlerted      bool
	}
	db.Raw("SELECT maintenance_interval, service_alerted FROM equipments WHERE id = ?", bike).Scan(&back)
	if back.MaintenanceInterval != 3000 || !back.ServiceAlerted {
		t.Errorf("down: %+v", back)
	}
}

// a service does the reminders of its equipment only; deleting it brings their previous cycle back
func TestServiceReminders(t *testing.T) {
	db := healthTestDB(t)
	if err := db.Exec("TRUNCATE service_reminders, maintenance_reminders, services, equipments, activities, users RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	user := Users{Username: "gear", Email: "gear@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	equipment := func(name string) Equipments {
		e := Equipments{Name: name, UserID: int(user.ID)}
		if err := db.CreateEquipment(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	bike, other := equipment("Gravel"), equipment("Road")
	reminder := func(e Equipments, name string) MaintenanceReminder {
		r := MaintenanceReminder{EquipmentID: e.ID, Name: name, EveryKm: 100, StartsAt: time.Now().AddDate(-1, 0, 0)}
		if err := db.CreateReminder(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	chain, brakes, foreign := reminder(bike, "chain"), reminder(bike, "brakes"), reminder(other, "chain")
	first, second := time.Now().AddDate(0, -2, 0).Truncate(time.Second), time.Now().AddDate(0, -1, 0).Truncate(time.Second)

	if err := db.CreateService(&Services{EquipmentID: bike.ID, Date: first, ReminderIDs: []uuid.UUID{chain.ID, foreign.ID}}); err != ErrUnknownReminder {
		t.Fatalf("a reminder of another equipment: %v", err)
	}
	var services int64
	db.Model(&Services{}).Count(&services)
	if services != 0 {
		t.Fatal("the refused service is saved")
	}
	old := Services{EquipmentID: bike.ID, Date: first, ReminderIDs: []uuid.UUID{chain.ID, brakes.ID, chain.ID}}
	if err := db.CreateService(&old); err != nil {
		t.Fatal(err)
	}
	recent := Services{EquipmentID: bike.ID, Date: second, ReminderIDs: []uuid.UUID{chain.ID}}
	if err := db.CreateService(&recent); err != nil {
		t.Fatal(err)
	}
	lastDone := func(r MaintenanceReminder) *time.Time {
		s, err := db.GetReminderStatus(bike.ID, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		return s.LastDone
	}
	if d := lastDone(chain); d == nil || !d.Equal(second) {
		t.Errorf("chain last done %v, want %v", d, second)
	}
	if d := lastDone(brakes); d == nil || !d.Equal(first) {
		t.Errorf("brakes last done %v, want %v", d, first)
	}
	list, err := db.GetServices(bike.ID)
	if err != nil || len(list) != 2 || len(list[0].ReminderIDs) != 1 || len(list[1].ReminderIDs) != 2 {
		t.Errorf("services %+v (%v)", list, err)
	}

	if err := db.DeleteService(bike.ID, recent.ID); err != nil {
		t.Fatal(err)
	}
	if d := lastDone(chain); d == nil || !d.Equal(first) {
		t.Errorf("after the deletion, chain last done %v, want %v", d, first)
	}
	// a deleted reminder keeps its services, the equipment deleted takes its reminders
	if err := db.DeleteReminder(bike.ID, brakes.ID); err != nil {
		t.Fatal(err)
	}
	if s, _ := db.GetService(bike.ID, old.ID); len(s.ReminderIDs) != 1 || s.ReminderIDs[0] != chain.ID {
		t.Errorf("after the reminder deletion: %+v", s)
	}
	if err := db.DeleteEquipment(bike.ID); err != nil {
		t.Fatal(err)
	}
	var left int64
	db.Model(&MaintenanceReminder{}).Count(&left)
	if left != 1 {
		t.Errorf("%d reminders left, want the one of the other equipment", left)
	}
}

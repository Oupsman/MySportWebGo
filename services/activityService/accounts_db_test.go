package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestDeleteUser: nothing of a deleted user is left, in any table nor on disk; the others keep theirs
func TestDeleteUser(t *testing.T) {
	db := emptyDB(t)
	t.Chdir(t.TempDir())
	admin := models.Users{Username: "admin", Email: "admin@test.local", Role: models.RoleAdmin}
	gone := models.Users{Username: "gone", Email: "gone@test.local", Role: models.RoleUser}
	other := models.Users{Username: "other", Email: "other@test.local", Role: models.RoleUser}
	for _, u := range []*models.Users{&admin, &gone, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(u, u.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	day := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	// everything a user can own, for gone and for other
	own := func(u models.Users) {
		t.Helper()
		bike := models.Equipments{Name: "bike", UserID: int(u.ID), DefaultSports: []string{"cycling"}}
		if err := db.CreateEquipment(&bike); err != nil {
			t.Fatal(err)
		}
		ride := saveRide(t, db, u, "cycling", day, 200, 200)
		route := models.Route{UserID: u.ID, Name: "loop", Sport: "cycling"}
		channel := models.Channel{UserID: uint64(u.ID), ChannelProvider: models.PushProvider, Name: "phone"}
		for _, row := range []interface{}{
			&models.Services{EquipmentID: bike.ID, Date: day, Mileage: 100},
			&route,
			&models.Devices{UserID: u.ID, Brand: "Garmin", Model: "Edge", Serial: u.Username},
			&models.DeviceTokens{UserID: u.ID, Name: "sync", Hash: u.Username, ExpiresAt: day.AddDate(1, 0, 0)},
			&channel,
			&models.HealthData{UserID: u.ID, Date: day, Weight: 70},
			&models.Segment{UserID: u.ID, Name: "climb", Sport: "cycling", Points: types.GpsPointArray{{Lat: 45, Lon: 6}, {Lat: 45.01, Lon: 6}}, SourceActivityID: &ride.ID},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatalf("%T: %v", row, err)
			}
		}
		if err := db.Create(&models.Notifications{ChannelID: uint64(channel.ID), Message: "service due"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(UserMediaDir(u.UUID), "Activities"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(UserMediaDir(u.UUID), "Activities", "ride.fit"), []byte("fit"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	own(gone)
	own(other)
	// gone was invited by the admin, and invited other
	adminID, goneID, otherID := admin.ID, gone.ID, other.ID
	used := day
	for _, inv := range []models.Invitations{
		{CreatedByID: &adminID, Hash: "a", ExpiresAt: day, UsedAt: &used, UsedByID: &goneID},
		{CreatedByID: &goneID, Hash: "b", ExpiresAt: day, UsedAt: &used, UsedByID: &otherID},
	} {
		if err := db.Create(&inv).Error; err != nil {
			t.Fatal(err)
		}
	}

	if _, err := DeleteUser(db, gone.ID, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	count := func(model interface{}, where string, args ...interface{}) int64 {
		t.Helper()
		var n int64
		if err := db.Unscoped().Model(model).Where(where, args...).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	for name, model := range map[string]interface{}{
		"activities": &models.Activity{}, "routes": &models.Route{}, "equipments": &models.Equipments{},
		"devices": &models.Devices{}, "device tokens": &models.DeviceTokens{}, "channels": &models.Channel{},
		"health data": &models.HealthData{}, "segments": &models.Segment{}, "equipment defaults": &models.EquipmentDefault{},
	} {
		if n := count(model, "user_id = ?", gone.ID); n != 0 {
			t.Errorf("%s: %d left", name, n)
		}
		if n := count(model, "user_id = ?", other.ID); n == 0 {
			t.Errorf("%s: the other user's are gone", name)
		}
	}
	if n := count(&models.Users{}, "id = ?", gone.ID); n != 0 {
		t.Error("the user is left")
	}
	if n := count(&models.Services{}, "1 = 1"); n != 1 {
		t.Errorf("%d services, want the other user's", n)
	}
	if n := count(&models.Notifications{}, "1 = 1"); n != 1 {
		t.Errorf("%d notifications, want the other user's", n)
	}
	if _, err := os.Stat(UserMediaDir(gone.UUID)); !os.IsNotExist(err) {
		t.Errorf("files left: %v", err)
	}
	if _, err := os.Stat(UserMediaDir(other.UUID)); err != nil {
		t.Errorf("the other user's files: %v", err)
	}
	// the invitations stay, without the deleted user
	invitations, err := db.GetInvitations()
	if err != nil || len(invitations) != 2 {
		t.Fatalf("invitations %v %v", invitations, err)
	}
	for _, inv := range invitations {
		switch inv.Hash {
		case "a":
			if inv.CreatedBy != "admin" || inv.UsedBy != models.DeletedUserName {
				t.Errorf("invitation used by the deleted user: %s, %s", inv.CreatedBy, inv.UsedBy)
			}
		case "b":
			if inv.CreatedBy != models.DeletedUserName || inv.UsedBy != "other" {
				t.Errorf("invitation of the deleted user: %s, %s", inv.CreatedBy, inv.UsedBy)
			}
		}
	}

	// the last admin stays
	if _, err := DeleteUser(db, admin.ID, zerolog.Nop()); !errors.Is(err, models.ErrLastAdmin) {
		t.Errorf("last admin: %v", err)
	}
	if _, err := DeleteUser(db, 999999, zerolog.Nop()); err == nil {
		t.Error("an unknown user deleted")
	}

}

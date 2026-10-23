package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
)

// the target weight was whole kilos (smallint): the migration makes it a decimal, keeping the targets
func TestWeightObjectiveMigration(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "target", Email: "target@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE users ALTER COLUMN weight_objective TYPE smallint").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE users SET weight_objective = 72 WHERE id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := models.CreateOrMigrate(db); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := db.Raw("SELECT data_type FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'weight_objective'").
		Scan(&kind).Error; err != nil {
		t.Fatal(err)
	}
	if kind != "double precision" && kind != "numeric" && kind != "real" {
		t.Fatalf("weight_objective is %s after the migration", kind)
	}
	got, err := db.GetUser(uint64(user.ID))
	if err != nil || got.WeightObjective != 72 {
		t.Fatalf("target after the migration: %v, %v", got.WeightObjective, err)
	}

	if err := db.SetWeightObjective(user.ID, 71.5); err != nil {
		t.Fatal(err)
	}
	if got, _ = db.GetUser(uint64(user.ID)); got.WeightObjective != 71.5 {
		t.Errorf("target %v, want 71.5", got.WeightObjective)
	}
}

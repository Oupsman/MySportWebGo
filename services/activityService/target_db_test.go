package activityService

import (
	"MySportWeb/internal/pkg/models"
	"testing"
)

// the target weight keeps its decimals (it was whole kilos before the versioned migrations)
func TestWeightObjectiveDecimals(t *testing.T) {
	db := testDB(t)
	user := models.Users{Username: "target", Email: "target@test.local"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.SetWeightObjective(user.ID, 71.5); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetUser(uint64(user.ID)); err != nil || got.WeightObjective != 71.5 {
		t.Errorf("target %v (%v), want 71.5", got.WeightObjective, err)
	}
}

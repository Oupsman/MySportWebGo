package models

import (
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const maxActivitiesColumns = 40

// ValidateActivitiesColumns checks the columns of the activities table: ids of the shape of the widget ids,
// each once. The web application knows the columns: an id it doesn't know is skipped there.
func ValidateActivitiesColumns(columns []string) error {
	if len(columns) > maxActivitiesColumns {
		return fmt.Errorf("at most %d columns", maxActivitiesColumns)
	}
	seen := make(map[string]bool, len(columns))
	for _, c := range columns {
		if !widgetID.MatchString(c) {
			return errors.New("a column id is 1 to 40 lowercase letters, digits, - or _")
		}
		if seen[c] {
			return fmt.Errorf("the column %s is twice", c)
		}
		seen[c] = true
	}
	return nil
}

// GetActivitiesColumns returns the columns of the activities table of the user, in their order; nil for
// the default ones
func (db *DB) GetActivitiesColumns(userID uint) ([]string, error) {
	var stored *string
	err := db.Model(&Users{}).Where("id = ?", userID).Select("activities_columns").Scan(&stored).Error
	if err != nil || stored == nil {
		return nil, err
	}
	var columns []string
	if err := json.Unmarshal([]byte(*stored), &columns); err != nil {
		return nil, err
	}
	return columns, nil
}

// SetActivitiesColumns saves the columns of the activities table of the user (validated), nil goes back to
// the default ones
func (db *DB) SetActivitiesColumns(userID uint, columns []string) error {
	var value interface{}
	if columns != nil {
		if err := ValidateActivitiesColumns(columns); err != nil {
			return err
		}
		data, err := json.Marshal(columns)
		if err != nil {
			return err
		}
		value = string(data)
	}
	return db.Model(&Users{}).Where("id = ?", userID).
		UpdateColumn("activities_columns", gorm.Expr("CAST(? AS jsonb)", value)).Error
}

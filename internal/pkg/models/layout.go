package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"gorm.io/gorm"
)

// DashboardWidget is a widget of the dashboard, as the user arranged it on a grid of 12 columns: at column X
// and row Y (from 0), W columns wide and H rows high, hidden when the user removed it. A layout saved before
// the grid has no X and Y, its widgets in their order, H in pixels (0: fits its content). The web application
// knows the widgets: an id it doesn't know is skipped there.
type DashboardWidget struct {
	ID     string `json:"id"`
	X      *int   `json:"x,omitempty"`
	Y      *int   `json:"y,omitempty"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Hidden bool   `json:"hidden"`
}

const (
	maxDashboardWidgets = 64
	dashboardColumns    = 12
	maxWidgetRows       = 100
	maxWidgetRow        = maxDashboardWidgets * maxWidgetRows
	// a layout saved before the grid
	maxWidgetHeight = 2000
)

var widgetID = regexp.MustCompile(`^[a-z0-9_-]{1,40}$`)

// ValidateDashboardLayout checks the widgets of a dashboard: known shapes, each id once, all on the grid or
// none (a layout saved before the grid)
func ValidateDashboardLayout(widgets []DashboardWidget) error {
	if len(widgets) > maxDashboardWidgets {
		return fmt.Errorf("at most %d widgets", maxDashboardWidgets)
	}
	seen := make(map[string]bool, len(widgets))
	onGrid := len(widgets) > 0 && widgets[0].X != nil
	for _, w := range widgets {
		if !widgetID.MatchString(w.ID) {
			return errors.New("a widget id is 1 to 40 lowercase letters, digits, - or _")
		}
		if seen[w.ID] {
			return fmt.Errorf("the widget %s is twice in the layout", w.ID)
		}
		seen[w.ID] = true
		if w.W < 1 || w.W > dashboardColumns {
			return fmt.Errorf("the width of the widget %s is 1 to %d columns", w.ID, dashboardColumns)
		}
		if (w.X != nil) != onGrid || (w.Y != nil) != onGrid {
			return errors.New("the widgets are all placed on the grid (x and y) or none")
		}
		if !onGrid {
			if w.H < 0 || w.H > maxWidgetHeight {
				return fmt.Errorf("the height of the widget %s is 0 (its content) to %d pixels", w.ID, maxWidgetHeight)
			}
			continue
		}
		if *w.X < 0 || *w.X+w.W > dashboardColumns {
			return fmt.Errorf("the widget %s is out of the %d columns", w.ID, dashboardColumns)
		}
		if *w.Y < 0 || *w.Y > maxWidgetRow {
			return fmt.Errorf("the row of the widget %s is 0 to %d", w.ID, maxWidgetRow)
		}
		if w.H < 1 || w.H > maxWidgetRows {
			return fmt.Errorf("the height of the widget %s is 1 to %d rows", w.ID, maxWidgetRows)
		}
	}
	return nil
}

// GetDashboardLayout returns the widgets of the dashboard of the user, nil for the default layout
func (db *DB) GetDashboardLayout(userID uint) ([]DashboardWidget, error) {
	var layout *string
	err := db.Model(&Users{}).Where("id = ?", userID).Select("dashboard_layout").Scan(&layout).Error
	if err != nil || layout == nil {
		return nil, err
	}
	var widgets []DashboardWidget
	if err := json.Unmarshal([]byte(*layout), &widgets); err != nil {
		return nil, err
	}
	return widgets, nil
}

// SetDashboardLayout saves the widgets of the dashboard of the user (validated), nil goes back to the
// default layout
func (db *DB) SetDashboardLayout(userID uint, widgets []DashboardWidget) error {
	var value interface{}
	if widgets != nil {
		if err := ValidateDashboardLayout(widgets); err != nil {
			return err
		}
		data, err := json.Marshal(widgets)
		if err != nil {
			return err
		}
		value = string(data)
	}
	return db.Model(&Users{}).Where("id = ?", userID).
		UpdateColumn("dashboard_layout", gorm.Expr("CAST(? AS jsonb)", value)).Error
}

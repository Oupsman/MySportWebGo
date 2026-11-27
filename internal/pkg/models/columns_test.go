package models

import (
	"fmt"
	"testing"
)

func TestValidateActivitiesColumns(t *testing.T) {
	many := make([]string, maxActivitiesColumns+1)
	for i := range many {
		many[i] = fmt.Sprintf("c%d", i)
	}
	for _, c := range []struct {
		columns []string
		ok      bool
	}{
		{[]string{}, true},
		{[]string{"date", "title", "avg_hr", "moving_time"}, true},
		{many[:maxActivitiesColumns], true},
		{many, false},
		{[]string{"date", "date"}, false},
		{[]string{"Date"}, false},
		{[]string{""}, false},
	} {
		if err := ValidateActivitiesColumns(c.columns); (err == nil) != c.ok {
			t.Errorf("%v: %v, want ok %v", c.columns, err, c.ok)
		}
	}
}

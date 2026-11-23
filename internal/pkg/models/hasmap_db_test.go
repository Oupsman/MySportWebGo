package models

import (
	"fmt"
	"strings"
	"testing"
)

// HasMapColumn tells the traces of two points at least, read or not
func TestHasMapColumn(t *testing.T) {
	db := healthTestDB(t)
	point := `{"lat":-89.12345678901234,"lon":-179.12345678901234}`
	long := "[" + strings.TrimSuffix(strings.Repeat(`{"lat":45.1,"lon":5.1},`, 2000), ",") + "]"
	for trace, want := range map[string]bool{
		"":                                      false, // NULL
		"[]":                                    false,
		"null":                                  false,
		"[" + point + "]":                       false, // the longest point alone
		`[{"lat":0,"lon":0},{"lat":1,"lon":1}]`: true,
		"[" + point + "," + point + "]":         true,
		long:                                    true,
	} {
		value := "NULL"
		if trace != "" {
			value = fmt.Sprintf("convert_to('%s', 'UTF8')", trace)
		}
		var got bool
		err := db.Raw("SELECT " + strings.TrimSuffix(HasMapColumn, " AS has_map") +
			" FROM (SELECT " + value + "::bytea AS public_gps_points) AS activities").Scan(&got).Error
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%.60s… (%d bytes): %v, want %v", trace, len(trace), got, want)
		}
	}
	if len("["+point+"]") > 66 {
		t.Errorf("a point of %d bytes: the threshold assumes 66 at most", len("["+point+"]"))
	}
}

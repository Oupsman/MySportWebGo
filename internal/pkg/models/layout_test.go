package models

import (
	"encoding/json"
	"testing"
)

func TestValidateDashboardLayout(t *testing.T) {
	for _, c := range []struct {
		layout string
		ok     bool
	}{
		{`[]`, true},
		// before the grid: an order, heights in pixels
		{`[{"id":"calendar","w":4,"h":0},{"id":"weekly","w":8,"h":420,"hidden":true}]`, true},
		{`[{"id":"calendar","w":4,"h":2001}]`, false},
		// on the grid: one as high as the two beside it
		{`[{"id":"readiness","x":8,"y":0,"w":4,"h":20},{"id":"calendar","x":0,"y":0,"w":8,"h":10},` +
			`{"id":"weekly","x":0,"y":10,"w":8,"h":10}]`, true},
		{`[{"id":"calendar","x":8,"y":0,"w":4,"h":100}]`, true},
		{`[{"id":"calendar","x":9,"y":0,"w":4,"h":10}]`, false},
		{`[{"id":"calendar","x":-1,"y":0,"w":4,"h":10}]`, false},
		{`[{"id":"calendar","x":0,"y":-1,"w":4,"h":10}]`, false},
		{`[{"id":"calendar","x":0,"y":6401,"w":4,"h":10}]`, false},
		{`[{"id":"calendar","x":0,"y":0,"w":4,"h":0}]`, false},
		{`[{"id":"calendar","x":0,"y":0,"w":4,"h":101}]`, false},
		{`[{"id":"calendar","x":0,"w":4,"h":10}]`, false},
		{`[{"id":"calendar","y":0,"w":4,"h":10}]`, false},
		// both kinds mixed
		{`[{"id":"calendar","x":0,"y":0,"w":4,"h":10},{"id":"weekly","w":8,"h":0}]`, false},
		{`[{"id":"weekly","w":8,"h":0},{"id":"calendar","x":0,"y":0,"w":4,"h":10}]`, false},
	} {
		var widgets []DashboardWidget
		if err := json.Unmarshal([]byte(c.layout), &widgets); err != nil {
			t.Fatal(err)
		}
		if err := ValidateDashboardLayout(widgets); (err == nil) != c.ok {
			t.Errorf("%s: %v, want ok %v", c.layout, err, c.ok)
		}
	}
}

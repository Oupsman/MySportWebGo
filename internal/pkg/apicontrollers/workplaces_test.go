package apicontrollers

import (
	"MySportWeb/internal/pkg/models"
	"math"
	"strings"
	"testing"
)

func TestWorkplaceInput(t *testing.T) {
	var w models.Workplace
	if err := (workplaceInput{Name: "  office ", Lat: 45.2, Lon: 5.7, Diameter: 300.4}).apply(&w); err != nil ||
		w.Name != "office" || w.Diameter != 300 || w.Lat != 45.2 {
		t.Errorf("valid input: %+v %v", w, err)
	}
	for name, in := range map[string]workplaceInput{
		"latitude":      {Lat: 91, Diameter: 300},
		"longitude":     {Lon: -181, Diameter: 300},
		"NaN":           {Lat: math.NaN(), Diameter: 300},
		"too small":     {Diameter: 5},
		"too large":     {Diameter: 50000},
		"no diameter":   {},
		"NaN diameter":  {Diameter: math.NaN()},
		"name too long": {Name: strings.Repeat("a", maxWorkplaceName+1), Diameter: 300},
	} {
		if err := in.apply(&w); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

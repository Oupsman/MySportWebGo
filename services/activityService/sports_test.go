package activityService

import (
	"MySportWeb/internal/pkg/models"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"
)

func TestSportNameCoversTheProfile(t *testing.T) {
	// every value of the enum of the FIT profile has a name, the others are generic
	named := 0
	for value := 0; value < 0xFF; value++ {
		sport := typedef.Sport(value)
		name := SportName(sport)
		if name == "" || strings.Contains(name, "Invalid") || strings.ContainsAny(name, " ()") {
			t.Errorf("sport %d: name %q", value, name)
		}
		if name != "generic" {
			named++
		}
	}
	// the sports of profile 21.218 without generic and all
	if named < 80 {
		t.Errorf("%d named sports, the FIT profile has 82", named)
	}
	for sport, want := range map[typedef.Sport]string{
		typedef.SportRunning:            "running",
		typedef.SportCrossCountrySkiing: "cross_country_skiing",
		typedef.Sport(88):               "canoeing", // unknown to the library before v0.27.3
		typedef.Sport(250):              "generic",  // unknown to the profile
		typedef.SportAll:                "generic",
		typedef.SportInvalid:            "generic", // missing from the file
	} {
		if got := SportName(sport); got != want {
			t.Errorf("sport %d: %q, want %q", sport, got, want)
		}
	}
}

func TestTitleOfOtherSports(t *testing.T) {
	morning := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	for sport, want := range map[string]string{
		"cross_country_skiing": "Morning cross country skiing",
		"hiit":                 "Morning HIIT",
		"generic":              "Morning activity",
		"e_biking":             "Morning e-bike ride",
		"canoeing":             "Morning canoeing",
	} {
		if got := Title(sport, morning, time.UTC, nil); got != want {
			t.Errorf("%s: %q, want %q", sport, got, want)
		}
	}
}

func TestSumAnalyzeKeepsAnySport(t *testing.T) {
	for _, tt := range []struct {
		sport typedef.Sport
		want  string
	}{
		{typedef.Sport(88), "canoeing"},
		{typedef.SportCrossCountrySkiing, "cross_country_skiing"},
		{typedef.Sport(250), "generic"}, // refused before: "Invalid sport"
		{typedef.SportInvalid, "generic"},
	} {
		path := multisportFile(t, triathlonFiles[1:2], []typedef.Sport{tt.sport}, 0)
		activity, err := SumAnalyze(path, models.Users{SecurityDistance: 500}, models.Equipments{})
		if err != nil {
			t.Errorf("sport %d: %v", tt.sport, err)
			continue
		}
		if activity.Sport != tt.want {
			t.Errorf("sport %d: %q, want %q", tt.sport, activity.Sport, tt.want)
		}
	}

	// the legs of a multisport file too
	path := multisportFile(t, triathlonFiles[:2], []typedef.Sport{typedef.Sport(88), typedef.Sport(250)}, 0)
	activity, err := SumAnalyze(path, models.Users{SecurityDistance: 500}, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Legs) != 2 || activity.Legs[0].Sport != "canoeing" || activity.Legs[1].Sport != "generic" {
		t.Errorf("legs %+v", activity.Legs)
	}
}

package activityService

import (
	"MySportWeb/internal/pkg/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
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

// rewrite copies a FIT file of prototypes/ after changing its activity
func rewrite(t *testing.T, name string, change func(*filedef.Activity)) string {
	f, err := os.Open(filepath.Join("../../prototypes", name))
	if err != nil {
		t.Skip("FIT files missing in prototypes/")
	}
	fit, err := decoder.New(f, decoder.WithIgnoreChecksum()).Decode()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	activity := filedef.NewActivity(fit.Messages...)
	change(activity)
	path := filepath.Join(t.TempDir(), name)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	rewritten := activity.ToFIT(nil)
	// the files of prototypes/ have developer fields, a protocol 2.0 feature
	if err := encoder.New(out, encoder.WithProtocolVersion(proto.V2)).Encode(&rewritten); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSumAnalyzeKeepsMeasuredPowerOfAnySport(t *testing.T) {
	path := rewrite(t, triathlonFiles[2], func(a *filedef.Activity) {
		a.Sessions[0].Sport = typedef.SportRunning
		a.Sessions[0].AvgPower = 250
		for i, r := range a.Records {
			r.Power = uint16(240 + i%20)
			r.Temperature = int8(20 + i%3)
		}
	})
	activity, err := SumAnalyze(path, models.Users{SecurityDistance: 500}, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Powers) == 0 || len(activity.Powers) != len(activity.PowerTS) || activity.AvgPower != 250 {
		t.Errorf("running power: %d powers, %d time stamps, average %d", len(activity.Powers), len(activity.PowerTS), activity.AvgPower)
	}
	for _, p := range activity.Powers {
		if p < 240 || p >= 260 {
			t.Fatalf("power %d: the measured one is kept, not estimated", p)
		}
	}
	if len(activity.Temperatures) == 0 || len(activity.Temperatures) != len(activity.TemperatureTimeStamps) {
		t.Errorf("%d temperatures, %d time stamps", len(activity.Temperatures), len(activity.TemperatureTimeStamps))
	}

	// without power meter, the power of a run isn't estimated
	path = rewrite(t, triathlonFiles[2], func(a *filedef.Activity) {
		a.Sessions[0].Sport = typedef.SportRunning
		a.Sessions[0].AvgPower = basetype.Uint16Invalid
	})
	activity, err = SumAnalyze(path, models.Users{SecurityDistance: 500}, models.Equipments{})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Powers) != 0 {
		t.Errorf("%d powers estimated for a run", len(activity.Powers))
	}
}

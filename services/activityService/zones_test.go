package activityService

import (
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/types"
	"reflect"
	"testing"
)

func TestTimeInZones(t *testing.T) {
	// reserve 190 - 60 = 130: the zones 2 to 5 start at 138, 151, 164 and 177 bpm
	user := models.Users{MaxHR: 190}
	hour := func(zone int) types.FloatArray {
		zones := make(types.FloatArray, models.HRZones)
		zones[zone] = 3600
		return zones
	}
	cases := []struct {
		name string
		hr   uint16
		want types.FloatArray
	}{
		{"below the resting heart rate", 50, hour(0)},
		{"half the reserve", 125, hour(0)},
		{"60% of the reserve is zone 2", 138, hour(1)},
		{"just below zone 3", 150, hour(1)},
		{"tempo, from 151", 151, hour(2)},
		{"threshold, from 164", 164, hour(3)},
		{"90% of the reserve is zone 5", 177, hour(4)},
		{"above the maximum", 200, hour(4)},
	}
	for _, c := range cases {
		zones, bounds := TimeInZones(steadyActivity(user, 60, c.hr), 60)
		if !reflect.DeepEqual(zones, c.want) {
			t.Errorf("%s (%d bpm): zones %v, want %v", c.name, c.hr, zones, c.want)
		}
		if want := (types.Uint16Array{60, 138, 151, 164, 177, 190}); !reflect.DeepEqual(bounds, want) {
			t.Errorf("%s: bounds %v, want %v", c.name, bounds, want)
		}
	}
}

func TestTimeInZonesSplitsAndSkipsGaps(t *testing.T) {
	user := models.Users{MaxHR: 190}
	activity := models.Activity{User: user, Date: trimpDate,
		// 10 s ending in zone 1, 20 s ending in zone 3, a 60 s dropout, 5 s ending in zone 5
		Hearts:          types.Uint16Array{100, 120, 160, 180, 185},
		HeartTimeStamps: types.FloatArray{0, 10, 30, 90, 95},
	}
	zones, _ := TimeInZones(activity, 60)
	if want := (types.FloatArray{10, 0, 20, 0, 5}); !reflect.DeepEqual(zones, want) {
		t.Errorf("zones %v, want %v", zones, want)
	}
}

func TestTimeInZonesWithoutData(t *testing.T) {
	zero := make(types.FloatArray, models.HRZones)
	cases := []struct {
		name     string
		activity models.Activity
		restHR   uint16
	}{
		{"no heart rate", models.Activity{User: models.Users{MaxHR: 190}, Date: trimpDate}, 60},
		{"maximum below the resting heart rate", steadyActivity(models.Users{MaxHR: 55}, 60, 125), 60},
		{"maximum unknown", steadyActivity(models.Users{}, 60, 125), 60},
		{"time stamps missing", models.Activity{User: models.Users{MaxHR: 190}, Date: trimpDate,
			Hearts: types.Uint16Array{120, 130}}, 60},
	}
	for _, c := range cases {
		zones, bounds := TimeInZones(c.activity, c.restHR)
		if !reflect.DeepEqual(zones, zero) || bounds != nil {
			t.Errorf("%s: zones %v bounds %v, want zeros and no bounds", c.name, zones, bounds)
		}
	}
}

func TestTimeInZonesCountsLikeTRIMP(t *testing.T) {
	// the zones cover the time the TRIMP counts: a higher resting heart rate moves time to lower zones
	user := models.Users{MaxHR: 190}
	activity := steadyActivity(user, 30, 150)
	low, _ := TimeInZones(activity, 50)  // x = 0.71: zone 3
	high, _ := TimeInZones(activity, 80) // x = 0.64: zone 2
	if low[2] != 1800 || high[1] != 1800 {
		t.Errorf("zones %v with a resting heart rate of 50, %v with 80", low, high)
	}
}

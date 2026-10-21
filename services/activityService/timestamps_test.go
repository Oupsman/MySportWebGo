package activityService

import (
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// every value of the sparse series must have its own time stamp, in increasing order
func TestSparseSeriesTimeStamps(t *testing.T) {
	if testing.Short() {
		t.Skip("decoding FIT files is slow")
	}
	files, _ := filepath.Glob(fitFilesPattern)
	checked := 0
	for _, file := range files {
		a, err := analyzeFile(file)
		if err != nil || len(a.TimeStamps) == 0 {
			continue
		}
		checked++
		name := filepath.Base(file)
		series := map[string][2]int{
			"gps":     {len(a.GpsPoints), len(a.GpsTimeStamps)},
			"heart":   {len(a.Hearts), len(a.HeartTimeStamps)},
			"cadence": {len(a.Cadences), len(a.CadenceTimeStamps)},
			"power":   {len(a.Powers), len(a.PowerTS)},
		}
		for serie, n := range series {
			if n[0] != n[1] {
				t.Errorf("%s: %d %s values but %d time stamps", name, n[0], serie, n[1])
			}
		}
		for serie, stamps := range map[string][]float64{"gps": a.GpsTimeStamps, "heart": a.HeartTimeStamps, "cadence": a.CadenceTimeStamps} {
			if !sort.Float64sAreSorted(stamps) {
				t.Errorf("%s: %s time stamps are not in increasing order", name, serie)
			}
		}
		power := make([]float64, len(a.PowerTS))
		for i, s := range a.PowerTS {
			power[i], _ = strconv.ParseFloat(s, 64)
		}
		if !sort.Float64sAreSorted(power) {
			t.Errorf("%s: power time stamps are not in increasing order", name)
		}
	}
	if checked == 0 {
		t.Skip("no FIT file in prototypes/")
	}
	t.Logf("%d activities checked", checked)
}

package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"MySportWeb/internal/pkg/models"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// the analyses are counted by result, the busy ones included
func TestAnalysisMetrics(t *testing.T) {
	file := "../../prototypes/" + sameRouteFiles[0] + ".fit"
	if _, err := os.Stat(file); err != nil {
		t.Skip("the FIT files of prototypes/ aren't there")
	}
	count := func(result string) float64 { return testutil.ToFloat64(metrics.Analyses.WithLabelValues(result)) }
	ok, failed, busy := count("ok"), count("error"), count("busy")

	if _, err := SumAnalyze(file, models.Users{}, models.Equipments{}); err != nil {
		t.Fatal(err)
	}
	if _, err := SumAnalyze("../../prototypes/missing.fit", models.Users{}, models.Equipments{}); err == nil {
		t.Fatal("a missing file analysed")
	}
	release := fillAnalysisSlots(t)
	if _, err := SumAnalyze(file, models.Users{}, models.Equipments{}); err != ErrBusy {
		t.Fatalf("all slots taken: %v", err)
	}
	release()

	for result, got := range map[string]float64{"ok": count("ok") - ok, "error": count("error") - failed, "busy": count("busy") - busy} {
		if got != 1 {
			t.Errorf("%s: %v, want 1", result, got)
		}
	}
}

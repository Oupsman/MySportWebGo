package status

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestQuantile(t *testing.T) {
	inf := math.Inf(1)
	before := map[float64]float64{0.1: 10, 0.5: 10, 1: 10, inf: 10}
	// 100 observations since: 50 under 0.1 s, 40 between 0.1 and 0.5, 10 between 0.5 and 1
	after := map[float64]float64{0.1: 60, 0.5: 100, 1: 110, inf: 110}
	// the 95th of 100: in (0.5, 1], 5 of its 10
	if q := quantile(0.95, before, after); math.Abs(q-0.75) > 1e-9 {
		t.Errorf("p95 %v, want 0.75", q)
	}
	// the 50th: the last of (0, 0.1]
	if q := quantile(0.5, before, after); math.Abs(q-0.1) > 1e-9 {
		t.Errorf("p50 %v, want 0.1", q)
	}
	if q := quantile(0.95, after, after); !math.IsNaN(q) {
		t.Errorf("no observation: %v", q)
	}
}

// a registry with the metrics the status reads, the backend's names
type fake struct {
	registry  *prometheus.Registry
	requests  *prometheus.CounterVec
	durations *prometheus.HistogramVec
	analyses  *prometheus.CounterVec
	memory    prometheus.Gauge
	backup    prometheus.Gauge
	readable  prometheus.Gauge
	backfill  *prometheus.CounterVec
	duration  *prometheus.GaugeVec
}

func newFake() *fake {
	f := &fake{registry: prometheus.NewRegistry()}
	f.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "mysportweb_http_requests_total"}, []string{"method", "route", "status"})
	f.durations = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "mysportweb_http_request_duration_seconds",
		Buckets: []float64{.1, .5, 1}}, []string{"method", "route"})
	f.analyses = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "mysportweb_analyses_total"}, []string{"result"})
	f.memory = prometheus.NewGauge(prometheus.GaugeOpts{Name: "process_resident_memory_bytes"})
	f.backup = prometheus.NewGauge(prometheus.GaugeOpts{Name: "mysportweb_backup_last_success_timestamp_seconds"})
	f.readable = prometheus.NewGauge(prometheus.GaugeOpts{Name: "mysportweb_backup_dir_readable"})
	f.backfill = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "mysportweb_backfill_errors_total"}, []string{"step"})
	f.duration = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "mysportweb_backfill_last_duration_seconds"}, []string{"step"})
	f.registry.MustRegister(f.requests, f.durations, f.analyses, f.memory, f.backup, f.readable, f.backfill, f.duration)
	return f
}

func (f *fake) get(n int, status string, seconds float64) {
	for i := 0; i < n; i++ {
		f.requests.WithLabelValues("GET", "/api/v1/user", status).Inc()
		f.durations.WithLabelValues("GET", "/api/v1/user").Observe(seconds)
	}
}

func level(r Report, name string) (string, string) {
	for _, c := range r.Checks {
		if c.Name == name {
			return c.Level, c.Message
		}
	}
	return "", ""
}

func TestReport(t *testing.T) {
	f := newFake()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	s := NewSampler(f.registry)
	s.now = func() time.Time { return now }
	f.readable.Set(1)
	f.backup.Set(float64(now.Add(-3 * time.Hour).Unix()))
	f.memory.Set(500 << 20)
	f.duration.WithLabelValues("draw the thumbnails of activities again").Set(12)

	// two hours ago: 100 requests already
	now = now.Add(-2 * time.Hour)
	f.get(100, "200", 0.05)
	if err := s.Sample(); err != nil {
		t.Fatal(err)
	}
	// a minute a sample in the last hour: 10 requests each, one 5xx, a slow one
	now = now.Add(time.Hour)
	for i := 0; i < 60; i++ {
		f.get(9, "200", 0.05)
		f.get(1, "500", 0.8)
		f.analyses.WithLabelValues("ok").Inc()
		if err := s.Sample(); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
	}
	f.backfill.WithLabelValues("draw the thumbnails of activities again").Inc()

	r, err := s.Build(Options{Version: "1.2.3", MemoryLimit: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	// the last hour: from the sample an hour ago (after its 10 requests)
	if r.LastHour.Requests != 590 || r.LastHour.Errors != 59 || r.LastHour.Analyses["ok"] != 59 {
		t.Errorf("last hour: %+v", r.LastHour)
	}
	// 10% of the reads at 0.8 s: the p95 in (0.5, 1]
	if p := r.LastHour.ReadsP95; p == nil || *p <= 0.5 || *p > 1 {
		t.Errorf("p95 %v", p)
	}
	// the day: since the first sample
	if r.LastDay.Requests != 600 || !r.LastDay.Since.Equal(now.Add(-2*time.Hour)) {
		t.Errorf("last day: %v requests since %v", r.LastDay.Requests, r.LastDay.Since)
	}
	if len(r.History) != 61 || r.History[1].Requests != 10 || r.History[1].Errors != 1 || r.History[1].Analyses != 1 {
		t.Errorf("history: %d points, the second %+v", len(r.History), r.History[1])
	}
	if lv, msg := level(r, "Server errors"); lv != Warning || !strings.Contains(msg, "59 server errors") {
		t.Errorf("server errors: %s %s", lv, msg)
	}
	if lv, msg := level(r, "Backups"); lv != OK || !strings.Contains(msg, "3 h ago.") {
		t.Errorf("backups: %s %s", lv, msg)
	}
	if lv, _ := level(r, "Memory"); lv != OK {
		t.Errorf("memory: %s", lv)
	}
	if lv, msg := level(r, "Backfills"); lv != Warning || !strings.Contains(msg, "thumbnails") {
		t.Errorf("backfills: %s %s", lv, msg)
	}
	// the worst first
	if r.Checks[0].Level != Warning {
		t.Errorf("first check %+v", r.Checks[0])
	}

	// an old backup, memory near the limit
	f.backup.Set(float64(now.Add(-30 * time.Hour).Unix()))
	f.memory.Set(900 << 20)
	r, _ = s.Build(Options{MemoryLimit: 1 << 30})
	if lv, _ := level(r, "Backups"); lv != Critical {
		t.Errorf("old backup: %s", lv)
	}
	if lv, _ := level(r, "Memory"); lv != Warning {
		t.Errorf("memory near the limit: %s", lv)
	}
	if r.Checks[0].Name != "Backups" {
		t.Errorf("critical first: %+v", r.Checks[0])
	}
	f.readable.Set(0)
	if r, _ := s.Build(Options{}); func() string { l, _ := level(r, "Backups"); return l }() != Unknown {
		t.Error("unreadable backups: not unknown")
	}
}

// the samples older than History are forgotten
func TestSamplerHistory(t *testing.T) {
	f := newFake()
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	s := NewSampler(f.registry)
	s.now = func() time.Time { return now }
	for i := 0; i < 26*60; i++ {
		s.Sample()
		now = now.Add(time.Minute)
	}
	if n := len(s.snapshot()); n != 24*60+1 {
		t.Errorf("%d samples kept, want %d", n, 24*60+1)
	}
}

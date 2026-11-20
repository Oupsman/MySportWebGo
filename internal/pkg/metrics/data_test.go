package metrics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// gather returns the gauges a collector reports, by name
func gather(t *testing.T, c prometheus.Collector) map[string]float64 {
	t.Helper()
	registry := prometheus.NewRegistry()
	registry.MustRegister(c)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]float64{}
	for _, family := range families {
		for _, m := range family.GetMetric() {
			values[family.GetName()] = m.GetGauge().GetValue()
		}
	}
	return values
}

func TestDataCollectorCounts(t *testing.T) {
	calls := 0
	var fail bool
	counts := func(context.Context) (int64, int64, error) {
		calls++
		if fail {
			return 0, 0, errors.New("database down")
		}
		return 3, 1200, nil
	}
	collector := NewDataCollector(counts, "")
	got := gather(t, collector)
	if got["mysportweb_users"] != 3 || got["mysportweb_activities"] != 1200 {
		t.Errorf("counts: %v", got)
	}
	if _, ok := got["mysportweb_backup_last_success_timestamp_seconds"]; ok {
		t.Error("backup metrics without backup directory")
	}
	// kept CountsTTL: a scrape doesn't count again
	gather(t, collector)
	if calls != 1 {
		t.Errorf("%d counts for two scrapes", calls)
	}
	// the database fails: no counts rather than stale or zero ones
	collector.countedAt = time.Time{}
	fail = true
	if got := gather(t, collector); len(got) != 0 {
		t.Errorf("database down: %v", got)
	}
}

func TestDataCollectorBackups(t *testing.T) {
	counts := func(context.Context) (int64, int64, error) { return 0, 0, nil }
	dir := t.TempDir()

	// the backup service never ran: readable, no success
	got := gather(t, NewDataCollector(counts, dir))
	if got["mysportweb_backup_dir_readable"] != 1 || got["mysportweb_backup_last_success_timestamp_seconds"] != 0 {
		t.Errorf("no backup yet: %v", got)
	}
	if _, ok := got["mysportweb_backup_last_size_bytes"]; ok {
		t.Error("a size without dump")
	}

	if err := os.MkdirAll(filepath.Join(dir, "db"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, size int, age time.Duration) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("db/old.dump", 100, 48*time.Hour)
	write("db/latest.dump", 300, time.Hour)
	write("db/running.dump.partial", 999, 0) // not a backup yet
	write(".last-success", 0, time.Hour)
	got = gather(t, NewDataCollector(counts, dir))
	if got["mysportweb_backup_last_size_bytes"] != 300 {
		t.Errorf("size of the latest dump: %v", got["mysportweb_backup_last_size_bytes"])
	}
	if age := time.Since(time.Unix(int64(got["mysportweb_backup_last_success_timestamp_seconds"]), 0)); age < 59*time.Minute || age > 61*time.Minute {
		t.Errorf("last success %v ago, want 1h", age)
	}

	// owned by another user (BACKUP_OWNER isn't the user of the backend)
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	if err := os.Chmod(filepath.Join(dir, "db"), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(dir, "db"), 0700)
	got = gather(t, NewDataCollector(counts, dir))
	if got["mysportweb_backup_dir_readable"] != 0 {
		t.Errorf("unreadable: %v", got)
	}
	if _, ok := got["mysportweb_backup_last_success_timestamp_seconds"]; ok {
		t.Error("a last success from an unreadable directory")
	}
}

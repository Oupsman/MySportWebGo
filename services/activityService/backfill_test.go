package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

func TestBackfillProgress(t *testing.T) {
	var buf bytes.Buffer
	saved := BackfillProgressInterval
	defer func() { BackfillProgressInterval = saved }()
	BackfillProgressInterval = 0
	ctx := WithBackfillStep(context.Background(), "test progress")
	remaining := func() float64 { return testutil.ToFloat64(metrics.BackfillRemaining.WithLabelValues("test progress")) }

	p := newBackfillProgress(ctx, zerolog.New(&buf), "segments", 4, "starting")
	if !strings.Contains(buf.String(), `"segments":4`) {
		t.Errorf("start %s without the count of segments", buf.String())
	}
	if remaining() != 4 {
		t.Errorf("remaining %v at the start, want 4", remaining())
	}
	extra := func(e *zerolog.Event) *zerolog.Event { return e.Int("without_file", 1) }
	p.done(1, extra)
	if remaining() != 3 {
		t.Errorf("remaining %v after 1, want 3", remaining())
	}
	p.done(4, extra) // the last one: the caller logs the end
	if remaining() != 0 {
		t.Errorf("remaining %v at the end, want 0", remaining())
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines, want the start and one progress:\n%s", len(lines), buf.String())
	}
	for _, want := range []string{`"done":1`, `"total":4`, `"percent":"25.0"`, `"without_file":1`, `"left":`} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("progress %s without %s", lines[1], want)
		}
	}
	// too soon after the last log: nothing logged, the metric still follows
	BackfillProgressInterval = time.Hour
	buf.Reset()
	p.done(2, extra)
	if buf.Len() != 0 {
		t.Errorf("progress logged too soon: %s", buf.String())
	}
	if remaining() != 2 {
		t.Errorf("remaining %v after 2, want 2", remaining())
	}

	// outside of a named step: no metric, no panic
	newBackfillProgress(context.Background(), zerolog.New(&buf), "activities", 1, "unnamed").done(1, nil)
}

package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"context"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

// BackfillProgressInterval is how often a backfill logs how far it is
var BackfillProgressInterval = 30 * time.Second

// backfillStepKey is the key of the name of the backfill step in its context
type backfillStepKey struct{}

// WithBackfillStep names the backfill step that runs with ctx: its progress is the
// mysportweb_backfill_remaining metric of that step
func WithBackfillStep(ctx context.Context, step string) context.Context {
	return context.WithValue(ctx, backfillStepKey{}, step)
}

// backfillProgress tells the progress of a backfill: in the log at its start, then at most every
// BackfillProgressInterval with an estimate of the time left; in the mysportweb_backfill_remaining metric of
// its step (see WithBackfillStep) after each item
type backfillProgress struct {
	logger  zerolog.Logger
	unit    string // what it goes through: "activities", "segments"
	total   int
	started time.Time
	logged  time.Time
	step    string // "" outside of a named step: no metric
}

func newBackfillProgress(ctx context.Context, logger zerolog.Logger, unit string, total int, msg string) *backfillProgress {
	now := time.Now()
	logger.Info().Int(unit, total).Msg(msg)
	step, _ := ctx.Value(backfillStepKey{}).(string)
	p := &backfillProgress{logger: logger, unit: unit, total: total, started: now, logged: now, step: step}
	p.remaining(total)
	return p
}

func (p *backfillProgress) remaining(left int) {
	if p.step != "" {
		metrics.BackfillRemaining.WithLabelValues(p.step).Set(float64(left))
	}
}

// done tells the progress after done items: the metric each time, the log when the last log is old enough,
// with the fields added by fields (nil: none)
func (p *backfillProgress) done(done int, fields func(*zerolog.Event) *zerolog.Event) {
	p.remaining(p.total - done)
	now := time.Now()
	if done >= p.total || now.Sub(p.logged) < BackfillProgressInterval {
		return
	}
	p.logged = now
	elapsed := now.Sub(p.started)
	left := time.Duration(float64(elapsed) / float64(done) * float64(p.total-done)).Round(time.Second)
	event := p.logger.Info().Int("done", done).Int("total", p.total).
		Str("percent", strconv.FormatFloat(float64(done)*100/float64(p.total), 'f', 1, 64)).
		Str("elapsed", elapsed.Round(time.Second).String()).Str("left", left.String())
	if fields != nil {
		event = fields(event)
	}
	event.Msg("backfill in progress")
}

// elapsed is the time since the start of the backfill, for its last log
func (p *backfillProgress) elapsed() string {
	return time.Since(p.started).Round(time.Second).String()
}

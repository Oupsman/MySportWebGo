// Package status tells the admins what goes on in the instance, from the metrics the backend keeps for
// Prometheus (metrics.Registry), read in the process: no Prometheus needed. A Sampler keeps a sample of them
// every SampleInterval over the last History, in memory: the trends of the last day, lost at a restart.
package status

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const (
	SampleInterval = time.Minute
	History        = 24 * time.Hour
)

// families is the metrics gathered, by name
type families map[string]*dto.MetricFamily

func gather(g prometheus.Gatherer) (families, error) {
	list, err := g.Gather()
	if err != nil && len(list) == 0 {
		return nil, err
	}
	// a collector failing (the database down) still leaves the others
	f := families{}
	for _, mf := range list {
		f[mf.GetName()] = mf
	}
	return f, nil
}

// matches tells whether a metric has the label values of want ("" a label it lacks)
func matches(m *dto.Metric, want map[string]string) bool {
	for name, value := range want {
		found := ""
		for _, l := range m.GetLabel() {
			if l.GetName() == name {
				found = l.GetValue()
			}
		}
		if found != value && !(strings.HasSuffix(value, "*") && strings.HasPrefix(found, strings.TrimSuffix(value, "*"))) {
			return false
		}
	}
	return true
}

func value(m *dto.Metric) float64 {
	switch {
	case m.Counter != nil:
		return m.Counter.GetValue()
	case m.Gauge != nil:
		return m.Gauge.GetValue()
	case m.Untyped != nil:
		return m.Untyped.GetValue()
	}
	return 0
}

// sum adds the values of the metrics of a family matching the labels (a value ending with * a prefix)
func (f families) sum(name string, labels map[string]string) float64 {
	total := 0.0
	if mf, ok := f[name]; ok {
		for _, m := range mf.GetMetric() {
			if matches(m, labels) {
				total += value(m)
			}
		}
	}
	return total
}

// has tells whether a family was gathered
func (f families) has(name string) bool {
	_, ok := f[name]
	return ok
}

// byLabel sums the values of a family per value of a label
func (f families) byLabel(name, label string) map[string]float64 {
	result := map[string]float64{}
	if mf, ok := f[name]; ok {
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == label {
					result[l.GetValue()] += value(m)
				}
			}
		}
	}
	return result
}

// buckets returns the cumulative counts of a histogram by upper bound, summed over its metrics matching labels
func (f families) buckets(name string, labels map[string]string) map[float64]float64 {
	result := map[float64]float64{}
	if mf, ok := f[name]; ok {
		for _, m := range mf.GetMetric() {
			if m.Histogram == nil || !matches(m, labels) {
				continue
			}
			for _, b := range m.Histogram.GetBucket() {
				result[b.GetUpperBound()] += float64(b.GetCumulativeCount())
			}
			result[math.Inf(1)] += float64(m.Histogram.GetSampleCount())
		}
	}
	return result
}

// quantile estimates the q quantile of the observations between two snapshots of cumulative buckets, linear
// in its bucket as Prometheus' histogram_quantile; NaN without observation
func quantile(q float64, before, after map[float64]float64) float64 {
	bounds := make([]float64, 0, len(after))
	for b := range after {
		bounds = append(bounds, b)
	}
	sort.Float64s(bounds)
	if len(bounds) == 0 {
		return math.NaN()
	}
	total := after[math.Inf(1)] - before[math.Inf(1)]
	if total <= 0 {
		return math.NaN()
	}
	rank := q * total
	lower, below := 0.0, 0.0
	for _, b := range bounds {
		count := after[b] - before[b]
		if count >= rank {
			if math.IsInf(b, 1) {
				// beyond the last bound: the last bound, as Prometheus
				return lower
			}
			if count == below {
				return b
			}
			return lower + (b-lower)*(rank-below)/(count-below)
		}
		lower, below = b, count
	}
	return lower
}

// sample is the cumulative values kept every SampleInterval
type sample struct {
	Time       time.Time
	Requests   float64
	Errors     float64 // 5xx
	Reads      map[float64]float64
	Analyses   map[string]float64 // by result
	Logins     map[string]float64
	Emails     float64 // failed
	Memory     float64 // resident, bytes
	CPU        float64 // seconds
	DBWait     float64 // requests that waited for a connection
	Busy       float64 // analyses and thumbnails refused, busy
	Backfilled float64 // backfill errors
}

func sampleOf(f families, at time.Time) sample {
	return sample{
		Time:       at,
		Requests:   f.sum("mysportweb_http_requests_total", nil),
		Errors:     f.sum("mysportweb_http_requests_total", map[string]string{"status": "5*"}),
		Reads:      f.buckets("mysportweb_http_request_duration_seconds", map[string]string{"method": "GET"}),
		Analyses:   f.byLabel("mysportweb_analyses_total", "result"),
		Logins:     f.byLabel("mysportweb_logins_total", "result"),
		Emails:     f.sum("mysportweb_emails_total", map[string]string{"result": "failed"}),
		Memory:     f.sum("process_resident_memory_bytes", nil),
		CPU:        f.sum("process_cpu_seconds_total", nil),
		DBWait:     f.sum("go_sql_wait_count_total", nil),
		Busy:       f.sum("mysportweb_analyses_total", map[string]string{"result": "busy"}) + f.sum("mysportweb_thumbnails_total", map[string]string{"result": "busy"}),
		Backfilled: f.sum("mysportweb_backfill_errors_total", nil),
	}
}

// Sampler keeps a sample of the metrics every SampleInterval, the ones of the last History
type Sampler struct {
	gatherer prometheus.Gatherer
	mu       sync.Mutex
	samples  []sample
	now      func() time.Time
}

func NewSampler(g prometheus.Gatherer) *Sampler {
	return &Sampler{gatherer: g, now: time.Now}
}

// Sample keeps a sample now, forgetting the ones older than History
func (s *Sampler) Sample() error {
	f, err := gather(s.gatherer)
	if err != nil {
		return err
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, sampleOf(f, now))
	cut := 0
	for cut < len(s.samples) && now.Sub(s.samples[cut].Time) > History {
		cut++
	}
	s.samples = s.samples[cut:]
	return nil
}

// Run samples every SampleInterval until ctx is done, at once first
func (s *Sampler) Run(ctx context.Context) {
	ticker := time.NewTicker(SampleInterval)
	defer ticker.Stop()
	for {
		_ = s.Sample()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Sampler) snapshot() []sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sample(nil), s.samples...)
}

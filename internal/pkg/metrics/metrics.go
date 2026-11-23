// Package metrics holds the Prometheus metrics of the backend, served by its metrics listener (see
// webserver.RunMetrics), never by the API. Their names start with mysportweb_; monitoring/ has the alert
// rules and the Grafana dashboard reading them.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const namespace = "mysportweb"

// Registry holds the metrics of the backend, and the ones of the Go runtime and of the process
var Registry = prometheus.NewRegistry()

var (
	// HTTPRequests counts the answered requests, by method, route (the pattern of gin: /api/v1/activity/:id,
	// "unmatched" for a path without route) and status
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "http_requests_total",
		Help: "HTTP requests answered, by method, route and status code.",
	}, []string{"method", "route", "status"})

	// HTTPDuration is the time to answer the requests, by method and route
	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace, Name: "http_request_duration_seconds",
		Help: "Time to answer HTTP requests, by method and route.",
		// from the API reads (ms) to the uploads (analysis and thumbnail: seconds)
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"method", "route"})

	// Analyses counts the analyses of FIT files, by result: ok, error (an unreadable file), busy (refused
	// after waiting for a slot)
	Analyses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "analyses_total",
		Help: "Analyses of FIT files, by result: ok, error, busy (no slot within the queue timeout).",
	}, []string{"result"})

	// AnalysisDuration is the time to analyse a FIT file, its wait for a slot excluded
	AnalysisDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace, Name: "analysis_duration_seconds",
		Help:    "Time to analyse a FIT file, the wait for a slot excluded.",
		Buckets: []float64{.05, .1, .25, .5, 1, 2, 5, 10, 30},
	})

	// Thumbnails counts the thumbnails drawn, by result: ok, error, busy
	Thumbnails = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "thumbnails_total",
		Help: "Thumbnails drawn, by result: ok, error, busy (no slot within the queue timeout).",
	}, []string{"result"})

	// ThumbnailDuration is the time to draw a thumbnail, its map tiles included
	ThumbnailDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace, Name: "thumbnail_duration_seconds",
		Help:    "Time to draw a thumbnail, the download of its map tiles included.",
		Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 15},
	})

	// MapTiles counts the map tiles asked for the thumbnails, by result: cache (from the directory),
	// download, stale (downloading failed, an expired one used), error (none)
	MapTiles = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "map_tiles_total",
		Help: "Map tiles of the thumbnails, by result: cache, download, stale (download failed, expired tile used), error.",
	}, []string{"result"})

	// Logins counts the login attempts, by result: success, failure (wrong username or password), locked
	// (too many failures for the username)
	Emails = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "emails_total",
		Help: "Emails sent, by kind (verification, password_reset, invitation, notification, test) and result: sent, failed.",
	}, []string{"kind", "result"})
	Logins = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "logins_total",
		Help: "Login attempts, by result: success, failure, locked.",
	}, []string{"result"})

	// Registrations counts the registrations, by result: user, admin (the setup of the first admin), closed
	// (REGISTRATION=closed, or invite without invitation), invitation (refused), setup_code (refused),
	// conflict (username or email taken)
	Registrations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "registrations_total",
		Help: "Registrations, by result: user, admin, closed, invitation, setup_code, conflict.",
	}, []string{"result"})

	// BackfillRunning is 1 while a step of the backfills of the start runs
	BackfillRunning = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Name: "backfill_running",
		Help: "1 while a backfill step of the start runs.",
	}, []string{"step"})

	// BackfillDuration is how long the last run of each backfill step took
	BackfillDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Name: "backfill_last_duration_seconds",
		Help: "Duration of the last run of each backfill step.",
	}, []string{"step"})

	// BackfillErrors counts the backfill steps that failed
	BackfillErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "backfill_errors_total",
		Help: "Backfill steps that failed.",
	}, []string{"step"})

	// BackfillRemaining is what a backfill step has left to do: activities, or segments for the re-match
	// of the segments; 0 when it is done, or had nothing to do
	BackfillRemaining = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Name: "backfill_remaining",
		Help: "Items (activities, segments) a backfill step of the start has left to do.",
	}, []string{"step"})

	// SegmentMatches counts the matchings of an activity with a segment, by trigger: create (a new segment),
	// upload, trim, backfill (a segment matched with older rules), reanalysis (an activity analysed again)
	SegmentMatches = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "segment_matches_total",
		Help: "Matchings of an activity with a segment, by trigger: create, upload, trim, backfill, reanalysis.",
	}, []string{"trigger"})

	// SegmentPasses counts the passes on segments those matchings found, by trigger
	SegmentPasses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Name: "segment_passes_total",
		Help: "Passes on segments found by the matchings, by trigger.",
	}, []string{"trigger"})

	// SegmentMatchDuration is the time to match an activity with a segment, its reading excluded
	SegmentMatchDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace, Name: "segment_match_duration_seconds",
		Help:    "Time to match an activity with a segment, the reading of the activity excluded.",
		Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5},
	})

	// BuildInfo is 1, with the version of the backend
	BuildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace, Name: "build_info",
		Help: "1, labelled with the version of the backend.",
	}, []string{"version"})

	// SchemaVersion is the version of the schema of the database
	SchemaVersion = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace, Name: "schema_version",
		Help: "Version of the schema of the database (its last migration).",
	})
)

func init() {
	Registry.MustRegister(
		Emails,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		HTTPRequests, HTTPDuration,
		Analyses, AnalysisDuration, Thumbnails, ThumbnailDuration, MapTiles,
		Logins, Registrations,
		BackfillRunning, BackfillDuration, BackfillErrors, BackfillRemaining,
		SegmentMatches, SegmentPasses, SegmentMatchDuration,
		BuildInfo, SchemaVersion,
	)
}

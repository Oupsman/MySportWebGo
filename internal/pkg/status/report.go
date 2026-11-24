package status

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Check levels
const (
	OK       = "ok"
	Warning  = "warning"
	Critical = "critical"
	Unknown  = "unknown"
)

// Check is a health check: something that works, or needs a look
type Check struct {
	Name    string `json:"name"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Window is what happened over a period: since Since, the start of the backend when it is more recent
type Window struct {
	Since    time.Time          `json:"since"`
	Requests float64            `json:"requests"`
	Errors   float64            `json:"errors"`    // 5xx
	ReadsP95 *float64           `json:"reads_p95"` // seconds, the GET requests; nil without
	Analyses map[string]float64 `json:"analyses"`  // FIT files analysed, by result: ok, error, busy
	Logins   map[string]float64 `json:"logins"`    // success, failure, locked
	Emails   float64            `json:"emails_failed"`
	Busy     float64            `json:"busy"`     // uploads and thumbnails refused: every slot taken
	DBWaits  float64            `json:"db_waits"` // requests that waited for a database connection
}

// Point is a SampleInterval of the history
type Point struct {
	Time     time.Time `json:"time"`
	Requests float64   `json:"requests"`
	Errors   float64   `json:"errors"`
	ReadsP95 *float64  `json:"reads_p95"`
	Memory   float64   `json:"memory"`   // resident, bytes
	CPU      float64   `json:"cpu"`      // cores used
	Analyses float64   `json:"analyses"` // FIT files analysed
}

// Backfill is a step of the backfills of the start
type Backfill struct {
	Step         string  `json:"step"`
	Running      bool    `json:"running"`
	Remaining    float64 `json:"remaining"`
	LastDuration float64 `json:"last_duration"` // seconds
	Errors       float64 `json:"errors"`
}

// Slots are the slots of a kind of work in use, out of their capacity
type Slots struct {
	InUse    float64 `json:"in_use"`
	Capacity float64 `json:"capacity"`
}

// Report is the state of the instance
type Report struct {
	Now        time.Time `json:"now"`
	Version    string    `json:"version"`
	StartedAt  time.Time `json:"started_at"`
	Schema     float64   `json:"schema"`
	Users      float64   `json:"users"`
	Activities float64   `json:"activities"`
	Memory     struct {
		Resident float64 `json:"resident"` // bytes
		Limit    float64 `json:"limit"`    // the container's, 0 without
	} `json:"memory"`
	CPU        float64 `json:"cpu"` // cores used over the last sample
	Goroutines float64 `json:"goroutines"`
	Database   struct {
		Open  float64 `json:"open"`
		InUse float64 `json:"in_use"`
		Max   float64 `json:"max"`
	} `json:"database"`
	Slots map[string]Slots `json:"slots"`
	Disk  *Disk            `json:"disk"`
	// the backups of the backup service: Readable false when the backend can't read them, LastSuccess nil
	// without one
	Backups struct {
		Configured  bool       `json:"configured"`
		Readable    bool       `json:"readable"`
		LastSuccess *time.Time `json:"last_success"`
		Size        float64    `json:"size"`
	} `json:"backups"`
	Backfills []Backfill         `json:"backfills"`
	Weather   map[string]float64 `json:"weather"` // requests since the start, by result
	LastHour  Window             `json:"last_hour"`
	LastDay   Window             `json:"last_day"`
	Checks    []Check            `json:"checks"`
	History   []Point            `json:"history"`
}

// Disk is the space of the filesystem of the media
type Disk struct {
	Path  string `json:"path"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

// Options is what the metrics don't tell
type Options struct {
	Version     string
	MemoryLimit int64  // the container's, 0 without
	MediaDir    string // its filesystem's free space
	BackupAge   time.Duration
}

// BackupMaxAge: a backup older than that, the backup service fails (its alert in Prometheus too)
const BackupMaxAge = 26 * time.Hour

// window compares the current sample with the oldest one of the last d
func window(samples []sample, current sample, d time.Duration) Window {
	from := current
	for _, s := range samples {
		if current.Time.Sub(s.Time) <= d {
			from = s
			break
		}
	}
	w := Window{Since: from.Time, Requests: current.Requests - from.Requests, Errors: current.Errors - from.Errors,
		Analyses: diff(current.Analyses, from.Analyses), Logins: diff(current.Logins, from.Logins),
		Emails: current.Emails - from.Emails, Busy: current.Busy - from.Busy, DBWaits: current.DBWait - from.DBWait}
	if p := quantile(0.95, from.Reads, current.Reads); !math.IsNaN(p) {
		w.ReadsP95 = &p
	}
	return w
}

func diff(after, before map[string]float64) map[string]float64 {
	result := map[string]float64{}
	for k, v := range after {
		result[k] = v - before[k]
	}
	return result
}

// Build reports the state of the instance from the metrics gathered now, the history of the sampler
func (s *Sampler) Build(opts Options) (Report, error) {
	f, err := gather(s.gatherer)
	if err != nil {
		return Report{}, err
	}
	now := s.now()
	samples := s.snapshot()
	current := sampleOf(f, now)

	var r Report
	r.Now, r.Version = now, opts.Version
	r.StartedAt = time.Unix(int64(f.sum("process_start_time_seconds", nil)), 0)
	r.Schema = f.sum("mysportweb_schema_version", nil)
	r.Users, r.Activities = f.sum("mysportweb_users", nil), f.sum("mysportweb_activities", nil)
	r.Memory.Resident, r.Memory.Limit = current.Memory, float64(opts.MemoryLimit)
	r.Goroutines = f.sum("go_goroutines", nil)
	r.Database.Open = f.sum("go_sql_open_connections", nil)
	r.Database.InUse = f.sum("go_sql_in_use_connections", nil)
	r.Database.Max = f.sum("go_sql_max_open_connections", nil)
	r.Slots = map[string]Slots{}
	used, capacity := f.byLabel("mysportweb_slots_in_use", "kind"), f.byLabel("mysportweb_slots_capacity", "kind")
	for kind, c := range capacity {
		r.Slots[kind] = Slots{InUse: used[kind], Capacity: c}
	}
	if opts.MediaDir != "" {
		r.Disk = diskOf(opts.MediaDir)
	}
	r.Backups.Configured = f.has("mysportweb_backup_dir_readable")
	r.Backups.Readable = f.sum("mysportweb_backup_dir_readable", nil) == 1
	if success := f.sum("mysportweb_backup_last_success_timestamp_seconds", nil); success > 0 {
		t := time.Unix(int64(success), 0)
		r.Backups.LastSuccess = &t
	}
	r.Backups.Size = f.sum("mysportweb_backup_last_size_bytes", nil)
	running := f.byLabel("mysportweb_backfill_running", "step")
	remaining := f.byLabel("mysportweb_backfill_remaining", "step")
	durations := f.byLabel("mysportweb_backfill_last_duration_seconds", "step")
	errors := f.byLabel("mysportweb_backfill_errors_total", "step")
	for step, d := range durations {
		r.Backfills = append(r.Backfills, Backfill{Step: step, Running: running[step] == 1, Remaining: remaining[step],
			LastDuration: d, Errors: errors[step]})
	}
	for step := range running {
		if _, ok := durations[step]; !ok {
			r.Backfills = append(r.Backfills, Backfill{Step: step, Running: running[step] == 1, Remaining: remaining[step],
				Errors: errors[step]})
		}
	}
	sort.Slice(r.Backfills, func(i, j int) bool { return r.Backfills[i].Step < r.Backfills[j].Step })
	r.Weather = f.byLabel("mysportweb_weather_requests_total", "result")
	r.LastHour = window(samples, current, time.Hour)
	r.LastDay = window(samples, current, History)

	// the history: what happened in each interval between two samples, the current one the last
	all := append(samples, current)
	for i := 1; i < len(all); i++ {
		a, b := all[i-1], all[i]
		seconds := b.Time.Sub(a.Time).Seconds()
		p := Point{Time: b.Time, Requests: b.Requests - a.Requests, Errors: b.Errors - a.Errors, Memory: b.Memory,
			Analyses: sumMap(b.Analyses) - sumMap(a.Analyses)}
		if seconds > 0 {
			p.CPU = (b.CPU - a.CPU) / seconds
		}
		if q := quantile(0.95, a.Reads, b.Reads); !math.IsNaN(q) {
			p.ReadsP95 = &q
		}
		r.History = append(r.History, p)
	}
	if len(r.History) > 0 {
		r.CPU = r.History[len(r.History)-1].CPU
	}
	r.Checks = checks(r)
	return r, nil
}

func sumMap(m map[string]float64) float64 {
	total := 0.0
	for _, v := range m {
		total += v
	}
	return total
}

func plural(n float64, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%.0f %ss", n, word)
}

func bytes(b float64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", b/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", b/(1<<10))
	}
}

func ago(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// checks tells what works and what needs a look, the worst first
func checks(r Report) []Check {
	var result []Check
	add := func(name, level, message string) { result = append(result, Check{name, level, message}) }

	switch b := r.Backups; {
	case !b.Configured:
		add("Backups", Unknown, "No backup directory given to the backend (BACKUP_DIR): their age can't be checked.")
	case !b.Readable:
		add("Backups", Unknown, "The backend can't read the backup directory: set MSW_BACKUP_OWNER to its user.")
	case b.LastSuccess == nil:
		add("Backups", Critical, "No successful backup yet.")
	case r.Now.Sub(*b.LastSuccess) > BackupMaxAge:
		add("Backups", Critical, "The last successful backup is "+ago(r.Now.Sub(*b.LastSuccess))+": the backup service fails.")
	default:
		message := "Last backup " + ago(r.Now.Sub(*b.LastSuccess))
		if b.Size > 0 {
			message += ", " + bytes(b.Size)
		}
		add("Backups", OK, message+".")
	}

	if e := r.LastHour.Errors; e > 0 {
		add("Server errors", Warning, plural(e, "server error")+" in the last hour: see the logs of the backend.")
	} else {
		add("Server errors", OK, "None in the last hour.")
	}

	if r.Memory.Limit > 0 {
		share := r.Memory.Resident / r.Memory.Limit
		message := fmt.Sprintf("%s of %s (%.0f%%).", bytes(r.Memory.Resident), bytes(r.Memory.Limit), share*100)
		if share > 0.85 {
			add("Memory", Warning, message+" Near the limit: raise MSW_BACKEND_MEMORY if it lasts.")
		} else {
			add("Memory", OK, message)
		}
	} else {
		add("Memory", Unknown, bytes(r.Memory.Resident)+", no limit: set MSW_BACKEND_MEMORY to protect the database.")
	}

	if d := r.Disk; d != nil && d.Total > 0 {
		free := float64(d.Free) / float64(d.Total)
		message := fmt.Sprintf("%s free of %s (%.0f%%).", bytes(float64(d.Free)), bytes(float64(d.Total)), free*100)
		switch {
		case free < 0.03 || d.Free < 512<<20:
			add("Disk", Critical, message+" The uploads will fail when it is full.")
		case free < 0.10 || d.Free < 2<<30:
			add("Disk", Warning, message)
		default:
			add("Disk", OK, message)
		}
	}

	var failed, running []string
	for _, b := range r.Backfills {
		if b.Errors > 0 {
			failed = append(failed, b.Step)
		}
		if b.Running {
			text := b.Step
			if b.Remaining > 0 {
				text += fmt.Sprintf(" (%.0f left)", b.Remaining)
			}
			running = append(running, text)
		}
	}
	switch {
	case len(failed) > 0:
		add("Backfills", Warning, "Failed: "+strings.Join(failed, ", ")+". See the logs of the backend.")
	case len(running) > 0:
		add("Backfills", OK, "Running: "+strings.Join(running, ", ")+".")
	default:
		add("Backfills", OK, "Done.")
	}

	if busy := r.LastHour.Busy; busy > 0 {
		add("Uploads", Warning, plural(busy, "upload")+" refused in the last hour: every analysis slot was taken for a minute.")
	} else {
		add("Uploads", OK, "None refused in the last hour.")
	}
	if waits := r.LastHour.DBWaits; waits > 0 {
		add("Database", Warning, plural(waits, "request")+" waited for a database connection in the last hour.")
	} else {
		add("Database", OK, fmt.Sprintf("%.0f of %.0f connections in use.", r.Database.InUse, r.Database.Max))
	}
	if locked := r.LastHour.Logins["locked"]; locked > 0 {
		add("Logins", Warning, plural(locked, "login")+" refused for too many failures in the last hour: someone guessing a password?")
	} else {
		add("Logins", OK, plural(r.LastHour.Logins["failure"], "failed login")+" in the last hour.")
	}
	if failed := r.LastHour.Emails; failed > 0 {
		add("Emails", Warning, plural(failed, "email")+" not sent in the last hour: check the SMTP settings (Send a test email).")
	}
	if limited := r.Weather["rate_limited"]; limited > 0 {
		add("Weather", Warning, "Open-Meteo refused "+plural(limited, "request")+" since the start: too many a day; the others at the next start.")
	}

	order := map[string]int{Critical: 0, Warning: 1, Unknown: 2, OK: 3}
	sort.SliceStable(result, func(i, j int) bool { return order[result[i].Level] < order[result[j].Level] })
	return result
}

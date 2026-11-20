package metrics

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// CountsTTL is how long the counts of the database are kept between two scrapes: they change slowly, a
// scrape mustn't count the activities each time
const CountsTTL = time.Minute

// Counts returns the counts of the database: users and activities
type Counts func(ctx context.Context) (users, activities int64, err error)

// DataCollector reports what the database holds and the state of the backups of the backup service
type DataCollector struct {
	counts    Counts
	backupDir string // "" without backup service: no backup metrics

	mu                 sync.Mutex
	users, activities  int64
	countedAt          time.Time
	countErr           error
	usersDesc          *prometheus.Desc
	activitiesDesc     *prometheus.Desc
	backupReadableDesc *prometheus.Desc
	backupSuccessDesc  *prometheus.Desc
	backupSizeDesc     *prometheus.Desc
}

// NewDataCollector reports the counts of counts, and the backups of backupDir ("" for none)
func NewDataCollector(counts Counts, backupDir string) *DataCollector {
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, nil, nil)
	}
	return &DataCollector{
		counts:         counts,
		backupDir:      backupDir,
		usersDesc:      desc("users", "Users of the instance."),
		activitiesDesc: desc("activities", "Activities of all the users."),
		backupReadableDesc: desc("backup_dir_readable",
			"1 when the backend can read the backup directory (BACKUP_DIR), 0 otherwise: the backup metrics are then missing."),
		backupSuccessDesc: desc("backup_last_success_timestamp_seconds",
			"Unix time of the last successful backup of the backup service, 0 when there was none."),
		backupSizeDesc: desc("backup_last_size_bytes", "Size of the most recent database dump."),
	}
}

func (d *DataCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- d.usersDesc
	ch <- d.activitiesDesc
	if d.backupDir != "" {
		ch <- d.backupReadableDesc
		ch <- d.backupSuccessDesc
		ch <- d.backupSizeDesc
	}
}

func (d *DataCollector) Collect(ch chan<- prometheus.Metric) {
	d.collectCounts(ch)
	if d.backupDir != "" {
		d.collectBackups(ch)
	}
}

// collectCounts reports the counts, counted again once CountsTTL old; nothing while the database fails
func (d *DataCollector) collectCounts(ch chan<- prometheus.Metric) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.countedAt) >= CountsTTL {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		users, activities, err := d.counts(ctx)
		cancel()
		d.countedAt, d.countErr = time.Now(), err
		if err == nil {
			d.users, d.activities = users, activities
		}
	}
	if d.countErr != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(d.usersDesc, prometheus.GaugeValue, float64(d.users))
	ch <- prometheus.MustNewConstMetric(d.activitiesDesc, prometheus.GaugeValue, float64(d.activities))
}

// collectBackups reports the time of the last success of the backup service (its .last-success file) and
// the size of the most recent dump
func (d *DataCollector) collectBackups(ch chan<- prometheus.Metric) {
	dumps, err := os.ReadDir(filepath.Join(d.backupDir, "db"))
	if err != nil && !os.IsNotExist(err) {
		// not mounted, or owned by another user than the backend (BACKUP_OWNER)
		ch <- prometheus.MustNewConstMetric(d.backupReadableDesc, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(d.backupReadableDesc, prometheus.GaugeValue, 1)

	var success float64
	if info, err := os.Stat(filepath.Join(d.backupDir, ".last-success")); err == nil {
		success = float64(info.ModTime().Unix())
	}
	ch <- prometheus.MustNewConstMetric(d.backupSuccessDesc, prometheus.GaugeValue, success)

	var latest os.FileInfo
	for _, dump := range dumps {
		if filepath.Ext(dump.Name()) != ".dump" {
			continue // .partial: not a backup yet
		}
		if info, err := dump.Info(); err == nil && (latest == nil || info.ModTime().After(latest.ModTime())) {
			latest = info
		}
	}
	if latest != nil {
		ch <- prometheus.MustNewConstMetric(d.backupSizeDesc, prometheus.GaugeValue, float64(latest.Size()))
	}
}

// SlotsInUse reports how many of the capacity slots of a kind of work (analysis, thumbnail) are taken
func SlotsInUse(kind string, used func() int, capacity int) {
	Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace, Name: "slots_in_use", Help: "Slots taken, by kind of work; see slots_capacity.",
		ConstLabels: prometheus.Labels{"kind": kind},
	}, func() float64 { return float64(used()) }))
	Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace, Name: "slots_capacity", Help: "Slots of each kind of work running at a time.",
		ConstLabels: prometheus.Labels{"kind": kind},
	}, func() float64 { return float64(capacity) }))
}

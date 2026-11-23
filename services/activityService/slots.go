package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"errors"
	"time"
)

// The heavy work runs a few at a time: a burst of uploads queues instead of exhausting the memory (a FIT
// file is decoded whole) or the tile server (the thumbnails download map tiles)
const MaxConcurrentThumbnails = 2

// MaxConcurrentAnalyses is how many files are prepared or analysed at a time: 4, fewer under a small memory
// limit (SetConcurrentAnalyses at the start)
var MaxConcurrentAnalyses = 4

// The memory budget of an analysis slot, and of the rest of the backend. Measured: preparing a GPX of
// convert.MaxDecompressed (3 days recorded every second) takes about 350 MB, analysing its FIT file about
// 310 MB. The rest of the backend (requests, thumbnails, caches) is an estimate.
const (
	analysisMemory = 384 << 20
	baseMemory     = 256 << 20
)

// AnalysesFor returns how many analyses may run at a time within a memory limit (bytes, 0 for none):
// 2 under 1 GB, 4 from 2 GB, 1 at least
func AnalysesFor(limit int64) int {
	if limit <= 0 {
		return 4
	}
	return max(1, min(4, int((limit-baseMemory)/analysisMemory)))
}

// SetConcurrentAnalyses changes how many analyses run at a time; at the start only, before any analysis
func SetConcurrentAnalyses(n int) {
	MaxConcurrentAnalyses = n
	analysisSlots = newSlots(n)
}

// QueueTimeout is how long a request waits for its turn before giving up with ErrBusy (a variable for the
// tests)
var QueueTimeout = time.Minute

// ErrBusy is returned when the work waited QueueTimeout for its turn: the client should try again later
var ErrBusy = errors.New("the server is busy, try again in a moment")

var (
	analysisSlots  = newSlots(MaxConcurrentAnalyses)
	thumbnailSlots = newSlots(MaxConcurrentThumbnails)
)

func init() {
	metrics.SlotsInUse("analysis", func() int { return len(analysisSlots) }, func() int { return cap(analysisSlots) })
	metrics.SlotsInUse("thumbnail", func() int { return len(thumbnailSlots) }, func() int { return MaxConcurrentThumbnails })
}

// slots lets n jobs run at a time
type slots chan struct{}

func newSlots(n int) slots {
	return make(slots, n)
}

// acquire waits up to timeout for a free slot, the returned function frees it
func (s slots) acquire(timeout time.Duration) (release func(), err error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case s <- struct{}{}:
		return func() { <-s }, nil
	case <-timer.C:
		return nil, ErrBusy
	}
}

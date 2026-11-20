package activityService

import (
	"MySportWeb/internal/pkg/metrics"
	"errors"
	"time"
)

// The heavy work runs a few at a time: a burst of uploads queues instead of exhausting the memory (a FIT
// file is decoded whole) or the tile server (the thumbnails download map tiles)
const (
	MaxConcurrentAnalyses   = 4
	MaxConcurrentThumbnails = 2
)

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
	metrics.SlotsInUse("analysis", func() int { return len(analysisSlots) }, MaxConcurrentAnalyses)
	metrics.SlotsInUse("thumbnail", func() int { return len(thumbnailSlots) }, MaxConcurrentThumbnails)
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

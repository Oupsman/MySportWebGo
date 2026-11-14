package middlewares

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter counts events per key (an IP, a username) in fixed windows: at most max per window. It is
// in memory: the counts are per process and start again at a restart.
type RateLimiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	counts    map[string]*count
	lastSweep time.Time
	now       func() time.Time // time.Now, replaced by the tests
}

type count struct {
	n     int
	reset time.Time // the end of the window
}

func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{max: max, window: window, counts: map[string]*count{}, now: time.Now}
}

// current returns the count of key in the current window, nil when it has none. The lock is held.
func (l *RateLimiter) current(key string, now time.Time) *count {
	// the windows ended are forgotten once per window: the map doesn't grow with the keys seen once
	if now.Sub(l.lastSweep) > l.window {
		for k, c := range l.counts {
			if !now.Before(c.reset) {
				delete(l.counts, k)
			}
		}
		l.lastSweep = now
	}
	c := l.counts[key]
	if c != nil && !now.Before(c.reset) {
		delete(l.counts, key)
		return nil
	}
	return c
}

// Blocked tells whether key reached the max in the current window, and how long until it ends
func (l *RateLimiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if c := l.current(key, now); c != nil && c.n >= l.max {
		return true, c.reset.Sub(now)
	}
	return false, 0
}

// Add counts an event of key
func (l *RateLimiter) Add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.add(key, l.now())
}

func (l *RateLimiter) add(key string, now time.Time) {
	c := l.current(key, now)
	if c == nil {
		c = &count{reset: now.Add(l.window)}
		l.counts[key] = c
	}
	c.n++
}

// Allow counts an event of key unless it reached the max, it returns false and how long until the end of
// the window then
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if c := l.current(key, now); c != nil && c.n >= l.max {
		return false, c.reset.Sub(now)
	}
	l.add(key, now)
	return true, 0
}

// Reset forgets the events of key
func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.counts, key)
}

// RateLimit refuses the requests of an IP above the limit of l, with 429. The IP is the one gin trusts:
// the X-Forwarded-For of the trusted proxies only.
func RateLimit(l *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ok, wait := l.Allow(c.ClientIP()); !ok {
			TooManyRequests(c, wait)
			return
		}
		c.Next()
	}
}

// TooManyRequests answers 429, telling when to try again
func TooManyRequests(c *gin.Context, wait time.Duration) {
	seconds := int(math.Ceil(wait.Seconds()))
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.AbortWithStatusJSON(http.StatusTooManyRequests,
		gin.H{"error": "too many attempts, try again in " + strconv.Itoa(seconds) + " seconds"})
}

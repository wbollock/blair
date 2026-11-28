// Package ratelimit provides in-memory token buckets keyed by string.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is a set of token buckets, one per key.
type Limiter struct {
	mu      sync.Mutex
	every   rate.Limit
	burst   int
	buckets map[string]*bucket
}

type bucket struct {
	l    *rate.Limiter
	seen time.Time
}

// New allows n events per period per key, with a burst of n.
func New(per time.Duration, n int) *Limiter {
	l := &Limiter{every: rate.Every(per / time.Duration(n)), burst: n, buckets: map[string]*bucket{}}
	go func() {
		for range time.Tick(10 * time.Minute) {
			l.mu.Lock()
			for k, b := range l.buckets {
				if time.Since(b.seen) > time.Hour {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

// Allow consumes a token for key and returns the wait until the next one when refused.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{l: rate.NewLimiter(l.every, l.burst)}
		l.buckets[key] = b
	}
	b.seen = time.Now()
	res := b.l.Reserve()
	if d := res.Delay(); d > 0 {
		res.Cancel()
		return false, d
	}
	return true, 0
}

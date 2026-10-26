package httpapi

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter — token bucket на IP-адрес клиента (RemoteAddr).
type RateLimiter struct {
	mu      sync.Mutex
	rps     float64
	burst   float64
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter: rps <= 0 отключает ограничение. Горутина очистки завершается по ctx.
func NewRateLimiter(ctx context.Context, rps float64, burst int) *RateLimiter {
	l := &RateLimiter{rps: rps, burst: float64(burst), buckets: map[string]*bucket{}}
	if l.burst < 1 {
		l.burst = 1
	}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				l.mu.Lock()
				for k, b := range l.buckets {
					if now.Sub(b.last) > 5*time.Minute {
						delete(l.buckets, k)
					}
				}
				l.mu.Unlock()
			}
		}
	}()
	return l
}

func (l *RateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rps
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *RateLimiter) middleware(next http.Handler) http.Handler {
	if l == nil || l.rps <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !l.allow(host) {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, errorBody{Error: "rate limit exceeded", Code: "rate_limited"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

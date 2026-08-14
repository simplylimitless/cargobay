package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements rate limiting middleware
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string]*rateInfo
	limit    int
	window   time.Duration
}

type rateInfo struct {
	count     int
	windowEnd time.Time
}

// NewRateLimiter creates a new RateLimiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string]*rateInfo),
		limit:    limit,
		window:   window,
	}

	// Start cleanup goroutine
	go rl.cleanupExpired()

	return rl
}

// cleanupExpired removes expired rate limit entries
func (rl *RateLimiter) cleanupExpired() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for key, info := range rl.requests {
			if now.After(info.windowEnd) {
				delete(rl.requests, key)
			}
		}
		rl.mu.Unlock()
	}
}

// GetRateLimitHeaders returns the rate limit headers
func GetRateLimitHeaders(info *rateInfo, limit int) http.Header {
	headers := make(http.Header)
	headers.Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
	headers.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", limit-info.count))
	headers.Set("X-RateLimit-Reset", fmt.Sprintf("%d", info.windowEnd.Unix()))
	return headers
}

// RateLimitMiddleware returns a rate limiting HTTP middleware
func RateLimitMiddleware(limit int, window time.Duration) func(http.Handler) http.Handler {
	rl := NewRateLimiter(limit, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Forwarded-For")
			if key == "" {
				key = r.RemoteAddr
			}

			rl.mu.Lock()
			now := time.Now()
			info, exists := rl.requests[key]

			if !exists || now.After(info.windowEnd) {
				info = &rateInfo{
					count:     1,
					windowEnd: now.Add(window),
				}
				rl.requests[key] = info
				rl.mu.Unlock()

				w.Header().Add("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
				w.Header().Add("X-RateLimit-Remaining", fmt.Sprintf("%d", limit-1))
				w.Header().Add("X-RateLimit-Reset", fmt.Sprintf("%d", info.windowEnd.Unix()))
				next.ServeHTTP(w, r)
				return
			}

			if info.count >= limit {
				rl.mu.Unlock()
				w.Header().Add("Retry-After", fmt.Sprintf("%d", int(info.windowEnd.Sub(now).Seconds())))
				w.Header().Add("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
				w.Header().Add("X-RateLimit-Remaining", "0")
				w.Header().Add("X-RateLimit-Reset", fmt.Sprintf("%d", info.windowEnd.Unix()))
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			info.count++
			rl.mu.Unlock()

			w.Header().Add("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			w.Header().Add("X-RateLimit-Remaining", fmt.Sprintf("%d", limit-info.count))
			w.Header().Add("X-RateLimit-Reset", fmt.Sprintf("%d", info.windowEnd.Unix()))
			next.ServeHTTP(w, r)
		})
	}
}

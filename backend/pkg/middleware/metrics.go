package middleware

import (
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// MetricsMiddleware tracks request metrics
var (
	requestsTotal      = make(map[string]int64)
	requestDuration    = make(map[string][]float64)
	startTime          = time.Now()
	mu                 sync.Mutex
	cacheStatsProvider func() (hits, misses, errors int64)
)

// RegisterCacheStatsProvider wires the cache package's real hit/miss/error
// counters into /metrics. Called once at startup from main.go.
func RegisterCacheStatsProvider(f func() (hits, misses, errors int64)) {
	cacheStatsProvider = f
}

// PrometheusMiddleware is an HTTP middleware for Prometheus metrics
func PrometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create response writer that captures status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		defer func() {
			duration := time.Since(start).Seconds()

			key := fmt.Sprintf("%s:%s", r.Method, r.URL.Path)
			status := fmt.Sprintf("%d", wrapped.statusCode)

			mu.Lock()
			requestsTotal[key]++
			requestsTotal[fmt.Sprintf("%s:%s:%s", r.Method, r.URL.Path, status)]++

			if durations, exists := requestDuration[key]; exists {
				requestDuration[key] = append(durations, duration)
			} else {
				requestDuration[key] = []float64{duration}
			}
			mu.Unlock()
		}()

		next.ServeHTTP(wrapped, r)
	})
}

// MetricsHandler serves Prometheus metrics
func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	var lines []string

	// Request counts
	lines = append(lines, "# HELP cargobay_requests_total Total number of requests")
	lines = append(lines, "# TYPE cargobay_requests_total counter")
	mu.Lock()
	for key, count := range requestsTotal {
		parts := fmt.Sprintf("%s", key)
		lines = append(lines, fmt.Sprintf(`cargobay_requests_total{method="%s",path="%s",status="%s"} %d`,
			parts, parts, parts, count))
	}
	mu.Unlock()

	// Duration metrics
	lines = append(lines, "# HELP cargobay_request_duration_seconds Request duration in seconds")
	lines = append(lines, "# TYPE cargobay_request_duration_seconds histogram")
	mu.Lock()
	for key, durations := range requestDuration {
		if len(durations) > 0 {
			sum := 0.0
			for _, d := range durations {
				sum += d
			}
			avg := sum / float64(len(durations))
			lines = append(lines, fmt.Sprintf(`cargobay_request_duration_seconds_sum{path="%s"} %.6f`, key, sum))
			lines = append(lines, fmt.Sprintf(`cargobay_request_duration_seconds_count{path="%s"} %d`, key, len(durations)))
			lines = append(lines, fmt.Sprintf(`cargobay_request_duration_seconds_avg{path="%s"} %.6f`, key, avg))
		}
	}
	mu.Unlock()

	// Cache metrics
	if cacheStatsProvider != nil {
		hits, misses, errs := cacheStatsProvider()

		lines = append(lines, "# HELP cargobay_cache_hits_total Cache hits")
		lines = append(lines, "# TYPE cargobay_cache_hits_total counter")
		lines = append(lines, fmt.Sprintf(`cargobay_cache_hits_total %d`, hits))

		lines = append(lines, "# HELP cargobay_cache_misses_total Cache misses")
		lines = append(lines, "# TYPE cargobay_cache_misses_total counter")
		lines = append(lines, fmt.Sprintf(`cargobay_cache_misses_total %d`, misses))

		lines = append(lines, "# HELP cargobay_cache_errors_total Cache errors")
		lines = append(lines, "# TYPE cargobay_cache_errors_total counter")
		lines = append(lines, fmt.Sprintf(`cargobay_cache_errors_total %d`, errs))

		total := hits + misses
		hitRate := 0.0
		if total > 0 {
			hitRate = float64(hits) / float64(total)
		}
		lines = append(lines, "# HELP cargobay_cache_hit_rate Cache hit rate (0-1)")
		lines = append(lines, "# TYPE cargobay_cache_hit_rate gauge")
		lines = append(lines, fmt.Sprintf(`cargobay_cache_hit_rate %.4f`, hitRate))
	}

	// Memory metrics
	lines = append(lines, "# HELP cargobay_memory_usage_bytes Memory usage in bytes")
	lines = append(lines, "# TYPE cargobay_memory_usage_bytes gauge")
	lines = append(lines, fmt.Sprintf(`cargobay_memory_usage_bytes %d`, memoryUsage()))

	// Uptime
	lines = append(lines, "# HELP cargobay_uptime_seconds Server uptime in seconds")
	lines = append(lines, "# TYPE cargobay_uptime_seconds counter")
	lines = append(lines, fmt.Sprintf(`cargobay_uptime_seconds %.2f`, time.Since(startTime).Seconds()))

	w.Write([]byte(fmt.Sprintf("%s\n", joinLines(lines))))
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

func memoryUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}


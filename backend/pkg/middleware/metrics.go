package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// MetricsMiddleware tracks request metrics
var (
	requestsTotal   = make(map[string]int64)
	requestDuration = make(map[string][]float64)
	cacheHits       int64
	cacheMisses     int64
	startTime       = time.Now()
	mu              sync.Mutex
)

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
	lines = append(lines, "# HELP cargobay_cache_hits_total Cache hits")
	lines = append(lines, "# TYPE cargobay_cache_hits_total counter")
	lines = append(lines, fmt.Sprintf(`cargobay_cache_hits_total %d`, cacheHits))

	lines = append(lines, "# HELP cargobay_cache_misses_total Cache misses")
	lines = append(lines, "# TYPE cargobay_cache_misses_total counter")
	lines = append(lines, fmt.Sprintf(`cargobay_cache_misses_total %d`, cacheMisses))

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
	// In production, use gopsutil or similar
	return 0
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

// RecordCacheHit records a cache hit
func RecordCacheHit() {
	mu.Lock()
	defer mu.Unlock()
	cacheHits++
}

// RecordCacheMiss records a cache miss
func RecordCacheMiss() {
	mu.Lock()
	defer mu.Unlock()
	cacheMisses++
}

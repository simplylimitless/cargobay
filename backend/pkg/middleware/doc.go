// Package middleware provides HTTP middleware components
//
// Middleware components:
//   - RateLimiter    - Per-IP rate limiting
//   - AuthMiddleware - JWT/Bearer token validation
//   - MetricsHandler - Prometheus metrics endpoint
//   - Tracing        - Request tracing
//
// Metrics exposed via /metrics endpoint:
//   - cargobay_requests_total
//   - cargobay_request_duration_seconds
//   - cargobay_cache_hits/misses_total
//   - cargobay_memory_usage_bytes
//   - cargobay_uptime_seconds
package middleware

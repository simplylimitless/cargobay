// Package cache provides Redis caching for stateless scaling
//
// Redis is used for:
//   - Response caching with TTL
//   - Session management
//   - Rate limiting state
//   - Distributed lock management
//
// Key methods:
//   - Get - Retrieve cached value
//   - Set - Store with TTL
//   - Delete - Invalidate key
//   - InvalidatePattern - Batch invalidation
package cache

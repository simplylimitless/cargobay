// Package proxy implements protocol-specific caching proxies
//
// Supported proxy modes:
//   - npm       - npm Registry API
//   - maven     - Maven Repository Layout
//   - docker    - Docker Registry v2 API
//   - pypi      - PyPI Simple API (planned)
//   - nuget     - NuGet V2/V3 API (planned)
//
// Each proxy handler:
//   - Caches responses from upstream
//   - Handles client requests transparently
//   - Manages cache invalidation
//   - Supports authentication when needed
package proxy

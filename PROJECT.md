# cargobay - Documentation

## Overview

**cargobay** is a stateless, horizontally scalable universal binary repository manager that acts as both:
1. **Caching Proxy** - Transparent proxy for npm, gradle/maven, and other package managers
2. **Artifact Repository** - Centralized storage for Docker images, AI/ML models, and custom artifacts

## Supported Proxy Modes

### npm Proxy
- Implements npm Registry API (`/npm/-/*` endpoints)
- Caches packages from registry.npmjs.org or private registries
- Supports scoped packages (`@scope/package`)
- Transparent to npm clients - no configuration changes needed

### Gradle/Maven Proxy
- Implements Maven Repository Layout (`/group/artifact/version/`)
- Caches JARs, POMs, and other Maven artifacts
- Supports both Gradle and Maven clients
- Handles repository metadata (`maven-metadata.xml`)

### Docker Registry Proxy
- Implements Docker Registry v2 API
- Caches images from Docker Hub and other registries
- Supports manifest lists and multi-arch images

### PyPI Proxy (Planned)
- Implements PyPI Simple API
- Caches Python wheels and source distributions

### NuGet Proxy (Planned)
- Implements NuGet V2/V3 APIs
- Caches .NET packages

## Technology Stack

### Backend (Go)
- **Go 1.21+** - High-performance, compiled language
- **chi/mux** - HTTP router
- **pgx** - PostgreSQL driver
- **redis** - Redis client for caching
- **prometheus/client_golang** - Metrics instrumentation
- **cobra** - CLI tooling

### Frontend (React + TypeScript)
- **React 18** - UI library
- **Vite** - Build tool
- **Tailwind CSS** - Styling
- **React Query** - Data fetching
- **React Router** - Routing

### Infrastructure
- **Kubernetes** - Orchestration
- **Helm** - Package management
- **Redis Cluster** - Caching/session
- **PostgreSQL** - Metadata database
- **S3/GCS/Azure Blob** - Artifact storage

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                          Kubernetes Ingress                          │
│                    (NGINX, Traefik, ALB, etc.)                       │
└──────────────────────────────────────────────────────────────────────┘
                                     │
        ┌────────────────────────────┼──────────────────────────────┐
        │                            │                              │
┌───────────────┐          ┌───────────────────┐          ┌───────────────────┐
│  cargobay-1   │          │   cargobay-2      │          │   cargobay-N      │
│  (Go Binary)  │          │   (Go Binary)     │          │   (Go Binary)     │
│               │          │                   │          │                   │
│  - HTTP       │          │   - HTTP          │          │   - HTTP          │
│  - Auth       │          │   - Auth          │          │   - Auth          │
│  - Rate Limit │          │   - Rate Limit    │          │   - Rate Limit    │
│  - Metrics    │          │   - Metrics       │          │   - Metrics       │
└───────────────┘          └───────────────────┘          └───────────────────┘
        │                            │                              │
        └────────────────────────────┼──────────────────────────────┘
                                     │
                          ┌──────────────────────┐
                          │   Redis Cluster      │
                          │   (Cache + Sessions) │
                          └──────────────────────┘
                                     │
                          ┌──────────────────────┐
                          │   Object Storage     │
                          │   (S3/NFS/GCS/Azure) │
                          │   (Artifacts)        │
                          └──────────────────────┘
                                     │
                          ┌──────────────────────┐
                          │   Database           │
                          │   (PostgreSQL)       │
                          │   (Metadata)         │
                          └──────────────────────┘
```

## Core Components

### 1. Server (`cmd/server/main.go`)

Main entry point for the Go server.

**Features:**
- Rate limiting middleware
- Prometheus metrics endpoint (`/metrics`)
- JWT authentication middleware
- Request tracing
- Graceful shutdown handling

**Endpoints:**
- `GET /health` - Health check (public)
- `GET /metrics` - Prometheus metrics
- `GET /api/*` - REST API for web UI
- `GET /npm/*` - npm Registry API (proxy mode)
- `GET /maven/*` - Maven Repository API (proxy mode)
- `GET /docker/*` - Docker Registry v2 API (proxy mode)

---

### 2. Configuration (`pkg/config/config.go`)

Configuration with YAML support and environment variable overrides.

**Configuration Structure:**
```go
type Config struct {
  Server   ServerConfig
  Storage  StorageConfig
  Database DatabaseConfig
  Cache    CacheConfig
  Registries []RegistryConfig
}

// Registry config for proxy mode
type RegistryConfig struct {
  ID       string
  Name     string
  URL      string
  Type     string // npm, maven, docker, pypi, nuget
  Proxy    bool   // Enable proxy mode
  Upstream string // Upstream registry to cache
}
```

**Environment Variables:**
- `PORT` - Server port (default: 4500)
- `REDIS_URL` - Redis connection string
- `DATABASE_URL` - PostgreSQL connection string
- `STORAGE_TYPE` - Storage backend
- `CONFIG_PATH` - Path to YAML config file

---

### 3. Database Layer (`pkg/database/database.go`)

PostgreSQL-backed database for metadata.

**Main Tables:**

| Table | Description |
|-------|-------------|
| `artifacts` | Artifact metadata with full-text search |
| `users` | User accounts with RBAC |
| `access_keys` | API keys for programmatic access |
| `registries` | Upstream registry configurations |
| `cache_entries` | Cached content metadata |
| `audit_log` | Audit trail for operations |

**Key Methods:**

| Method | Description |
|--------|-------------|
| `GetArtifact()` | Fetch artifact by registry, namespace, name, version |
| `GetArtifactByDigest()` | Fetch artifact by content digest |
| `ListArtifacts()` | List artifacts with pagination/filtering |
| `SearchArtifacts()` | Full-text search across artifacts |
| `SaveArtifact()` | Upsert artifact metadata |
| `CreateUser()` | Create new user account |
| `CreateAccessKey()` | Generate API access key |

---

### 4. Storage Layer (`pkg/storage/`)

Storage adapter interface for artifact content.

**Backends:**
- **S3Adapter** - AWS S3
- **GCSAdapter** - Google Cloud Storage
- **NFSAdapter** - Network File System
- **AzureAdapter** - Azure Blob Storage
- **LocalAdapter** - Local file system

**Interface:**
```go
type StorageAdapter interface {
  SaveArtifact(path string, data []byte) error
  GetArtifact(path string) ([]byte, error)
  DeleteArtifact(path string) error
  ArtifactExists(path string) (bool, error)
  ListArtifacts(prefix string) ([]string, error)
}
```

---

### 5. Proxy Handlers (`pkg/proxy/`)

Protocol-specific proxy handlers.

**npm Proxy (`pkg/proxy/npm/`):**
- Implements npm Registry API
- Caches packages with automatic TTL
- Handles scoped packages
- Supports publish operations (when configured)

**Gradle/Maven Proxy (`pkg/proxy/maven/`):**
- Implements Maven Repository Layout
- Handles POM files and dependencies
- Generates `maven-metadata.xml` dynamically
- Supports snapshot versions

**Docker Registry Proxy (`pkg/proxy/docker/`):**
- Implements Docker Registry v2 API
- Caches image manifests and layers
- Handles multi-arch images
- Supports notary signatures

---

### 6. Redis Cache (`pkg/cache/redis.go`)

Redis-based caching for horizontal scaling.

**Features:**
- Automatic key expiration (TTL)
- Connection pooling
- Key pattern matching for cache invalidation

**Methods:**
- `Get()` - Retrieve cached value
- `Set()` - Store value with TTL
- `Delete()` - Delete key
- `InvalidatePattern()` - Delete by pattern

---

### 7. Rate Limiting (`pkg/middleware/ratelimit.go`)

Rate limiting middleware for API protection.

**Features:**
- Per-IP rate limiting (configurable)
- Sliding window algorithm
- Request throttling for heavy operations

**Headers:**
- `X-RateLimit-Limit` - Maximum requests per window
- `X-RateLimit-Remaining` - Remaining requests
- `X-RateLimit-Reset` - Reset timestamp

---

### 8. Prometheus Metrics (`pkg/middleware/metrics.go`)

Prometheus metrics instrumentation.

**Metrics Exposed:**
- `cargobay_requests_total` - Request counts by method, path, status
- `cargobay_request_duration_seconds` - Request duration histogram
- `cargobay_cache_hits_total` - Cache hit count
- `cargobay_cache_misses_total` - Cache miss count
- `cargobay_memory_usage_bytes` - Heap memory usage
- `cargobay_uptime_seconds` - Server uptime

---

## Usage

### As an npm Proxy

**npm configuration:**
```bash
# Configure npm to use cargobay
npm config set registry http://cargobay:4500/npm/

# Or in .npmrc
registry=http://cargobay:4500/npm/
```

### As a Gradle/Maven Proxy

**Gradle configuration:**
```groovy
repositories {
    maven {
        url 'http://cargobay:4500/maven/'
    }
}
```

**Maven configuration (settings.xml):**
```xml
<settings>
  <mirrors>
    <mirror>
      <id>cargobay</id>
      <url>http://cargobay:4500/maven/</url>
      <mirrorOf>*</mirrorOf>
    </mirror>
  </mirrors>
</settings>
```

### As a Docker Registry Proxy

**Docker configuration:**
```bash
# Pull through cargobay
docker pull cargobay:4500/library/nginx:latest

# Or configure as a registry mirror in /etc/docker/daemon.json
{
  "registry-mirrors": ["http://cargobay:4500/"]
}
```

## Testing

### Unit Tests
```bash
go test ./pkg/...
```

### Integration Tests
```bash
go test -tags=integration ./test/integration/...
```

## Deployment

### Docker
```bash
docker build -t cargobay:latest .
docker run -p 4500:4500 -v ./config.yaml:/config.yaml cargobay
```

### Kubernetes
```bash
helm install cargobay ./charts/cargobay
```

## License

MIT

# cargobay Architecture

This document describes the internal architecture and component interactions of cargobay.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        cargobay Server                                      │
│                        (Go + React SPA)                                     │
│                        Port 4500 (default)                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                  HTTP Server (chi Router)                            │   │
│  │                                                                      │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────┐ │   │
│  │  │   API Layer │  │  Proxy      │  │  Web UI     │  │ Metrics │ │   │
│  │  │  (/api/v1)  │  │  Handlers   │  │  (embedded) │  │ (/metrics)│ │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └───────────┘ │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                    Core Services Layer                               │   │
│  │                                                                      │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────┐ │   │
│  │  │   RBAC      │  │  Auth       │  │  Cache      │  │Search     │ │   │
│  │  │  Manager    │  │  Service    │  │  (Redis)    │  │Index      │ │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └───────────┘ │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                    Data Layer                                        │   │
│  │                                                                      │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────┐ │   │
│  │  │PostgreSQL   │  │  Storage    │  │  Redis      │  │Vuln DB    │ │   │
│  │  │(Metadata)   │  │  Adapter    │  │  (Cache)    │  │Updater    │ │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └───────────┘ │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
   ┌────▼────┐          ┌─────▼─────┐          ┌────▼─────┐
   │ Upstream│          │ Upstream  │          │ Upstream │
   │ Docker  │          │   npm     │          │  Maven   │
   │ registry│          │registry   │          │ registry │
   └─────────┘          └───────────┘          └──────────┘
```

## Component Breakdown

### 1. API Layer (`backend/pkg/api/`)

The API layer handles all REST endpoints under `/api/v1/` and exposes public proxy paths.

**Main Components:**
- `server.go` - Chi router with all route definitions and request handlers
- `middleware/auth.go` - Authentication middleware with Bearer/Basic support
- `middleware/path.go` - Path normalization and routing helpers
- `middleware/metrics.go` - Prometheus metrics collection

**Key Endpoints:**
| Path | Method | Description | Auth |
|------|--------|-------------|------|
| `/api/v1/health` | GET | Health check | Public |
| `/api/v1/version` | GET | Server version | Public |
| `/api/v1/metrics` | GET | Prometheus metrics | Public |
| `/api/v1/auth/login` | POST | User authentication | Public |
| `/api/v1/auth/logout` | POST | Session invalidation | Required |
| `/api/v1/setup/status` | GET | Check first-run setup | Public |
| `/api/v1/setup` | POST | Initialize first admin user | Public (once only) |
| `/api/v1/artifacts` | GET | List artifacts (cursor pagination) | Required |
| `/api/v1/artifacts` | POST | Upload artifact | Required |
| `/api/v1/artifacts/{id}` | GET | Get artifact details | Required |
| `/api/v1/artifacts/{id}/download` | GET | Download artifact | Required |
| `/api/v1/artifacts/{id}/scan` | POST | Trigger vulnerability scan | Required |
| `/api/v1/artifacts/{id}/sign` | POST | Sign artifact | Required |
| `/api/v1/search` | GET | Full-text search | Required |
| `/api/v1/search/autocomplete` | GET | Search suggestions | Required |
| `/api/v1/registries` | GET | List accessible registries | Required |
| `/api/v1/registries` | POST | Create registry | Required |
| `/api/v1/registries/{id}` | DELETE | Delete registry | Required |
| `/api/v1/users` | GET | List users | Required |
| `/api/v1/users/{id}/roles` | POST | Assign role to user | Required |
| `/api/v1/roles` | GET | List roles | Required |
| `/api/v1/settings/vulnerability-db` | GET/PUT | Vuln DB settings | Required |
| `/api/v1/settings/search-index` | GET/PUT | Search index settings | Required |

**Middleware Chain:**
```
NormalizePath → Recoverer → Heartbeat → Logger → PrometheusMiddleware
   → AuthMiddleware → (Route-specific auth/permission checks)
```

### 2. Proxy Layer (`backend/pkg/proxy/`)

The proxy layer implements language/package manager-specific registry protocols.

**Supported Proxies (21 total):**
| Proxy | Path | Protocol | Package Type |
|-------|------|----------|--------------|
| Docker | `/v2` | Docker Registry v2 | OCI/Docker images |
| npm | `/npm` | npm Registry API | Node.js packages |
| Maven | `/maven` | Maven Central API | Java/JAR/WAR |
| Gradle | `/gradle` | Maven-compatible | Java/Kotlin |
| SBT | `/sbt` | Maven-compatible | Scala |
| PyPI | `/pypi` | PEP 503 | Python wheels/sdists |
| NuGet | `/nuget` | NuGet V2/V3 | .NET packages |
| Helm | `/helm` | Chart Repository | Kubernetes charts |
| Cargo | `/cargo` | crates.io API | Rust crates |
| Go Modules | `/go` | go.mod protocol | Go modules |
| Alpine | `/alpine` | APKINDEX | Alpine Linux packages |
| Debian | `/debian` | apt repository | Debian/Ubuntu packages |
| RPM | `/rpm` | RPM metadata | RHEL/CentOS packages |
| YUM | `/yum` | yum metadata | RHEL/CentOS packages |
| Conan | `/conan` | Conan API | C/C++ packages |
| CocoaPods | `/cocoapods` | CocoaPods API | iOS/macOS pods |
| Swift | `/swift` | Swift Package Index | Swift packages |
| Dart | `/dart` | Pub API | Dart packages |
| Terraform | `/terraform` | Terraform providers | Terraform modules |
| Composer | `/composer` | Packagist API | PHP packages |
| Conda | `/conda` | Conda API | Conda packages |

**Proxy Architecture:**
```
┌────────────────────────────────────────────────────────────┐
│                    Docker Proxy                            │
├────────────────────────────────────────────────────────────┤
│  - Handles manifest pulls/pushes                           │
│  - Handles layer uploads (blobs)                           │
│  - Supports manifest lists (multi-arch)                    │
│  - Caches image metadata in Redis                          │
│  - Stores layer content in configured storage              │
│  - Implements Docker Registry v2 API spec                  │
└────────────────────────────────────────────────────────────┘
```

**Proxy Flow:**
1. Client requests package from cargobay
2. Proxy checks Redis cache for metadata
3. If cache miss, fetches from upstream registry
4. Caches metadata in Redis
5. Streams content from storage (or upstream if not cached)
6. Returns response to client

### 3. Authentication & Authorization (`backend/pkg/auth/`, `backend/pkg/rbac/`)

**Auth Service (`auth.go`):**
- Password hashing (bcrypt with configurable cost)
- Access key generation (UUID + timestamp)
- Session management with configurable TTL
- Login/logout with access key invalidation
- HTTP Basic auth for Docker clients

**RBAC Manager (`rbac.go`):**
- Role-based permission enforcement
- 5 built-in roles with configurable permissions
- Registry access grants (per-user, per-registry)
- Permission caching for performance

**Roles:**
| Role | Permissions | Use Case |
|------|-------------|----------|
| `admin` | All operations including user management | System administrators |
| `operator` | Artifact operations, registry management, scan triggering | DevOps/Platform teams |
| `developer` | Read/write artifacts they created | Individual developers |
| `viewer` | Read-only access to artifacts and registries | QA/auditors |
| `auditor` | Read audit logs and vulnerability reports | Security/compliance |

**Permissions Format:** `resource:action` (e.g., `artifact:read`, `artifact:write`, `registry:write`, `user:admin`, `system:read`)

### 4. Data Layer

#### PostgreSQL (`backend/pkg/database/`)

Stores all metadata with connection pooling optimized for stateless deployments.

**Key Tables:**
| Table | Purpose | Key Fields |
|-------|---------|------------|
| `artifacts` | Artifact metadata | id, registry_id, artifact_type, namespace, name, version, digest, size, tags, signatures, metadata |
| `artifact_signatures` | Digital signatures | artifact_id, type, key_id, timestamp, verified |
| `users` | User accounts | id, username, email, password_hash, is_active, last_login |
| `user_roles` | User ↔ Role mapping | user_id, role_id |
| `roles` | Role definitions | id, name, description, is_system |
| `permissions` | Permission definitions | id, name, description, resource, action, is_system |
| `role_permissions` | Role ↔ Permission mapping | role_id, permission_id |
| `registries` | Registry configurations | id, name, url, type, proxy, enabled, private, host, upstream_auth_type |
| `registry_access` | User grants for private registries | registry_id, user_id, can_read, can_publish |
| `audit_logs` | Action audit trail | id, user_id, action, resource_type, resource_id, details |
| `vulnerability_scans` | Scan results | artifact_id, severity, vulnerabilities (JSONB), scanned_by |
| `vuln_db_settings` | Vulnerability DB configuration | auto_update_enabled, update_interval_hours, last_checked_at |
| `search_index_settings` | Search configuration | auto_reindex_enabled, reindex_interval_hours, last_reindexed_at |
| `access_keys` | API access keys | id, user_id, key_hash, permissions, expires_at, is_active |

**Database Features:**
- Connection pooling with configurable limits (default: 10 connections)
- Query result caching via Redis
- Full-text search with tsvector column
- Cursor-based pagination for large datasets
- Soft deletes for users (is_active flag)

#### Storage Adapters (`backend/pkg/storage/`)

Content-addressable storage for artifact bytes.

**Supported Backends:**
| Backend | Type | Configuration |
|---------|------|---------------|
| Local | Filesystem | `storage.path` |
| S3 | Object storage | `bucket`, `region`, `access_key`, `secret_key` |
| GCS | Object storage | `bucket`, `credentials_file` |
| Azure | Blob storage | `account_name`, `account_key`, `container` |

**Storage Path Format:**
```
{registryID}/{artifactType}/{namespace}/{artifactName}/{version}/{digest}
```

**Features:**
- Content-addressable storage (digest-based paths)
- Concurrent upload/download support
- Artifact integrity verification
- Path-based organization

#### Redis Cache (`backend/pkg/cache/`)

Caches frequently accessed data with configurable TTL.

**Cache Keys:**
| Key Pattern | Value Type | TTL |
|-------------|------------|-----|
| `artifact:{registry}:{ns}:{name}:{version}` | Artifact metadata | 5 min |
| `catalog:{registry}:{type}` | Repository list | 5 min |
| `session:{token}` | User session | Session TTL |
| `ratelimit:{ip}` | Rate limit counter | 1 min |

### 5. Vulnerability Scanning (`backend/pkg/vulnerability/`)

**Scanner (`scanner.go`):**
- Scans artifacts for known vulnerabilities
- Supports Trivy (remote), Grype (local)
- Stores scan results in PostgreSQL (JSONB)
- Severity classification (critical, high, medium, low)
- Per-artifact-type scanning (Docker, npm, Maven, PyPI)

**Scanner Flow:**
```
Upload Artifact
    ↓
Trigger Scan (API or auto)
    ↓
Get Artifact Content from Storage
    ↓
Run Scanner (Trivy/Grype/npm-audit/etc.)
    ↓
Parse Results
    ↓
Store in vulnerability_scans Table
    ↓
Update Artifact Metadata with Scan Status
```

**DB Updater (`updater.go`):**
- Periodically updates vulnerability database
- Uses Trivy's vulnerability DB
- Configurable update interval (default: 24 hours)
- Last success/failure tracking
- Background scheduler with context cancellation

### 6. Search Index (`backend/pkg/searchindex/`)

**Reindexer (`reindexer.go`):**
- Maintains PostgreSQL full-text search index
- Rebuilds `search_vector` tsvector column
- Auto-reindexing on a schedule
- Manual reindex trigger via API
- Tracks last reindex timestamp and artifact count

**Search Features:**
- Full-text search across artifact metadata
- Autocomplete suggestions
- Filter by registry, type, namespace
- Cursor-based pagination

### 7. Web UI (`frontend/`, `backend/pkg/webui/`)

**Frontend (`frontend/`):**
- React 18 + TypeScript
- Vite build tool
- React Router for client-side routing
- Tailwind CSS for styling
- Playwright for E2E tests

**Component Structure:**
```
frontend/src/
├── components/       # Reusable UI components
│   ├── Navbar.tsx    # Top navigation bar
│   ├── Breadcrumbs.tsx  # Navigation breadcrumbs
│   └── SearchInput.tsx  # Search input with autocomplete
├── pages/            # Page-level components
│   ├── Login.tsx
│   ├── Setup.tsx     # First-run setup wizard
│   ├── GettingStarted.tsx
│   ├── RegistryList.tsx
│   ├── RegistryDetail.tsx
│   ├── ArtifactList.tsx
│   ├── ArtifactDetail.tsx
│   ├── ArtifactVersion.tsx
│   ├── ArtifactUpload.tsx
│   ├── ArtifactBrowse.tsx
│   ├── SearchResults.tsx
│   ├── Settings.tsx
│   ├── Profile.tsx
│   ├── AuditLogs.tsx
│   ├── VulnerabilityResults.tsx
│   └── UserEdit.tsx
├── context/          # React Context providers
│   └── AuthContext.tsx  # Auth state management
├── hooks/            # Custom hooks
│   └── useConfirm.tsx  # Confirmation dialog hook
└── __tests__/        # Jest + React Testing Library tests
```

**SPA Routing:**
```tsx
<Routes>
  <Route path="/setup" element={<Setup />} />
  <Route path="/" element={<RegistryList />} />
  <Route path="/registries/:registryId" element={<RegistryDetail />} />
  <Route path="/registries/:registryId/:artifactType" element={<ArtifactList />} />
  <Route path="/registries/:registryId/:artifactType/:namespace/:artifactName" element={<ArtifactDetail />} />
  <Route path="/registries/:registryId/:artifactType/:namespace/:artifactName/:version" element={<ArtifactVersion />} />
  <Route path="/search" element={<SearchResults />} />
  <Route path="/settings" element={<Settings />} />
  {/* ... more routes ... */}
</Routes>
```

**Embedded at Build:**
```go
// backend/pkg/webui/webui.go
//go:embed dist/*
var distFS embed.FS
```

### 8. Replication (`backend/pkg/replication/`)

**ReplicationService:**
- Cross-region and cross-cloud replication
- Multi-master support
- Conflict resolution
- Region configuration (primary, secondary, edge)

**Region Types:**
| Type | Description |
|------|-------------|
| primary | Master region with write access |
| secondary | Read replica that syncs from primary |
| edge | Regional edge cache for latency reduction |

## Data Flow Examples

### Artifact Upload Flow

```
Client → POST /api/v1/artifacts
    ↓
API Layer (auth check, RBAC permission check)
    ↓
Generate artifact ID, set metadata (uploadedBy, uploadedAt)
    ↓
Database: INSERT INTO artifacts
    ↓
Storage Adapter: PUT content-addressable blob
    ↓
Cache: Invalidate affected entries
    ↓
Vulnerability Scanner: Queue for scan
    ↓
Response: 201 Created with artifact metadata
```

### Docker Pull Flow

```
Client → GET /v2/<repo>/manifests/<tag>
    ↓
Docker Proxy (checks cache first)
    ↓
Redis: Lookup manifest digest
    ↓
Cache MISS → Upstream Registry (cache response)
    ↓
Database: Record artifact metadata (if first pull)
    ↓
Storage: Get layer content
    ↓
Response: Manifest + layer blobs
```

### Search Flow

```
Client → GET /api/v1/search?q=<query>
    ↓
API Layer (permission check, registry filter)
    ↓
Database: FULLTEXT SEARCH artifacts USING search_vector
    ↓
Return matching artifacts with cursor pagination
```

### Vulnerability Scan Flow

```
Trigger Scan (API or auto-schedule)
    ↓
Get Artifact from Storage
    ↓
Run Scanner (Trivy/Grype/npm-audit)
    ↓
Parse Results → Severity Classification
    ↓
Store in vulnerability_scans Table
    ↓
Update Artifact Metadata
```

## Deployment Architectures

### Development (Docker Compose)

```
┌─────────────────────────────────────────────────────────────────┐
│                         Docker Compose                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  cargobay-backend:4500                                          │
│  ├── API + Proxies                                              │
│  ├── Web UI (embedded)                                          │
│  └── Connects to:                                               │
│       ├── PostgreSQL:5432 (metadata)                            │
│       └── Redis:6379 (cache)                                    │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**docker-compose.yml Services:**
```yaml
services:
  backend:
    image: cargobay:latest
    ports:
      - "4500:4500"
    environment:
      - DATABASE_URL=postgres://postgres:5432/cargobay
      - REDIS_URL=redis://redis:6379
    depends_on:
      - postgres
      - redis

  postgres:
    image: postgres:15

  redis:
    image: redis:7-alpine
```

### Production (Kubernetes)

```
┌─────────────────────────────────────────────────────────────────┐
│                      Kubernetes Cluster                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Ingress (Nginx/Traefik)                                        │
│       │                                                          │
│       ├──→ cargobay-1:4500                                      │
│       ├──→ cargobay-2:4500  (stateless replicas)               │
│       └──→ cargobay-3:4500                                      │
│                                                                 │
│       └──→ PostgreSQL (StatefulSet)                             │
│       └──→ Redis (StatefulSet)                                  │
│       └──→ S3/GCS/Azure (External)                              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Kubernetes Components:**
- Deployments: cargobay replicas (stateless)
- StatefulSets: PostgreSQL, Redis
- External Services: S3/GCS/Azure (object storage)
- Ingress: Nginx/Traefik for load balancing

## Environment Variables

### Core Configuration
| Variable | Description | Example |
|----------|-------------|---------|
| `CONFIG_FILE` | Path to YAML config | `./config.yaml` |
| `PORT` | Server port (fixed to 4500) | `4500` |
| `HOST` | Server host | `0.0.0.0` |
| `DATABASE_URL` | PostgreSQL connection | `postgres://user:pass@host:5432/db` |
| `REDIS_URL` | Redis connection | `redis://host:6379` |
| `STORAGE_TYPE` | Storage backend | `s3`, `gcs`, `azure`, `local` |
| `TRIVY_CACHE_DIR` | Trivy DB cache | `/app/trivy-cache` |

### S3 Configuration
| Variable | Description |
|----------|-------------|
| `S3_ACCESS_KEY` | AWS access key |
| `S3_SECRET_KEY` | AWS secret key |

## Build Process

```
1. Frontend Build (Vite)
   └─> frontend/dist/ (static assets)

2. Backend Build (Go)
   ├─> Embed frontend/dist/ into binary
   ├─> Compile CGO (if needed)
   └─> Output: cargobay binary

3. Docker Build (Multi-stage)
   ├─> Stage 1: Build frontend
   ├─> Stage 2: Build backend (embeds dist/)
   └─> Stage 3: Alpine runtime with embedded binary
```

**Dockerfile Structure:**
```dockerfile
# Stage 1: Build frontend
FROM node:20 AS frontend
WORKDIR /app/frontend
COPY frontend/ .
RUN npm install && npm run build

# Stage 2: Build backend
FROM golang:1.21 AS backend
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o cargobay ./backend/cmd/server

# Stage 3: Runtime
FROM alpine:latest
COPY --from=backend /app/cargobay /usr/local/bin/
COPY --from=frontend /app/frontend/dist /app/dist
EXPOSE 4500
CMD ["cargobay"]
```

## Testing Strategy

### Unit Tests
- `backend/pkg/*/test.go` - Go unit tests
- `frontend/src/__tests__/*.test.tsx` - React component tests
- `backend/pkg/middleware/middleware_test.go` - Middleware tests
- `backend/pkg/proxy/*/test.go` - Proxy tests

### Integration Tests
- `backend/pkg/integration/integration_test.go` - Full stack tests
- `tests/e2e/api_test.go` - End-to-end API tests

### Test Coverage
- RBAC permission checks
- Proxy protocol compliance
- Storage adapter operations
- Database queries with caching
- Vulnerability scanning

## Performance Considerations

### Caching Strategy
- Metadata cached in Redis (5 min TTL)
- Artifact content stored in S3/GCS/Azure
- Docker manifest cache in Redis
- Connection pool caching

### Connection Pooling
- PostgreSQL: 10 connections per instance (configurable)
- Redis: Connection pooling via client

### Horizontal Scaling
- Stateless design (no local state)
- Shared Redis for sessions/cache
- Shared storage for artifact content
- Database for coordination
- Cursor-based pagination for large datasets

## Security Model

### Authentication
- Session-based (cookies)
- Bearer token (Authorization header)
- HTTP Basic auth (Docker clients)
- Access key expiration

### Authorization
- Role-based (5 built-in roles)
- Registry-level grants (private registries)
- Artifact ownership (users can delete own artifacts)
- System-level permissions (admin-only)

### Data Protection
- Passwords hashed (bcrypt)
- Secrets never returned via API
- Audit logging for all actions
- Signature verification support
- TLS termination at ingress

## Configuration Files

### YAML Configuration (`config.yaml`)

```yaml
server:
  port: 4500
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s

database:
  type: postgres
  dsn: postgresql://localhost:5432/cargobay

cache:
  type: redis
  url: redis://localhost:6379
  ttl: 1h
  max_size: 1073741824  # 1GB

storage:
  type: local
  config:
    path: /var/lib/cargobay/storage

registries:
  - id: dockerhub
    name: Docker Hub
    type: docker
    url: https://registry-1.docker.io
    proxy: true
    enabled: true
    priority: 1
```

## Metrics (Prometheus)

**Available Metrics:**
| Metric | Type | Description |
|--------|------|-------------|
| `cargobay_requests_total` | Counter | Request counts by method/path/status |
| `cargobay_request_duration_seconds` | Histogram | Request duration |
| `cargobay_cache_hits_total` | Counter | Cache hits |
| `cargobay_cache_misses_total` | Counter | Cache misses |
| `cargobay_memory_usage_bytes` | Gauge | Memory usage |
| `cargobay_uptime_seconds` | Counter | Server uptime |
| `cargobay_artifacts_total` | Gauge | Total artifacts |
| `cargobay_registries_total` | Gauge | Total registries |
| `cargobay_users_total` | Gauge | Total users |

## Supported Artifact Types

| Type | Format | Proxy | Upload | Scan |
|------|--------|-------|--------|------|
| Docker/OCI | .tar.gz | Yes | Yes | Yes |
| npm | .tgz | Yes | Yes | Yes |
| Maven | .jar/.war/.pom | Yes | Yes | Yes |
| PyPI | .whl/.tar.gz | Yes | Yes | Yes |
| NuGet | .nupkg | Yes | Yes | Yes |
| Helm | .tgz | Yes | Yes | No |
| Cargo | .crate | Yes | Yes | No |
| Go Modules | go.mod | Yes | Yes | No |
| Alpine | .apk | Yes | Yes | No |
| Debian | .deb | Yes | Yes | No |
| RPM | .rpm | Yes | Yes | No |
| Conan | .tar.gz | Yes | Yes | No |
| CocoaPods | .podspec | Yes | Yes | No |
| Swift | .zip | Yes | Yes | No |
| Dart | .pub | Yes | Yes | No |
| Terraform | .zip | Yes | Yes | No |
| Composer | .zip | Yes | Yes | No |
| Conda | .conda | Yes | Yes | No |

## Feature Status

### Completed Features
- [x] Multi-format proxy support (21 package types)
- [x] Artifact storage (S3/GCS/Azure/Local)
- [x] PostgreSQL metadata storage
- [x] Redis caching layer
- [x] RBAC with 5 roles
- [x] Vulnerability scanning (Trivy/Grype)
- [x] Full-text search with autocomplete
- [x] Audit logging
- [x] Prometheus metrics
- [x] Cross-region replication
- [x] Digital signature support
- [x] Self-service user setup
- [x] API key management

### In Progress
- [ ] Advanced replication with conflict resolution
- [ ] SSO integration (SAML, OAuth2, OpenID Connect)
- [ ] CDN integration for edge caching

### Planned
- [ ] Semantic search integration
- [ ] AI-powered artifact recommendations
- [ ] Multi-tenancy with organization isolation

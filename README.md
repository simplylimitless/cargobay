# cargobay - Universal Binary Repository Manager

cargobay is a stateless, horizontally scalable universal binary repository manager for storing, organizing, and distributing software artifacts.

## Supported Artifact Types (21 formats)

### Package Managers
- **npm** - Node.js packages (.tgz)
- **Maven/Gradle/SBT** - Java/Kotlin artifacts (JAR, WAR, POM)
- **PyPI** - Python packages (wheels, sdists)
- **NuGet** - .NET packages (.nupkg)
- **Cargo** - Rust crates (.crate)
- **Go Modules** - Go modules
- **Composer** - PHP packages
- **Conda** - Conda packages (.conda)
- **Dart** - Dart packages (.pub)
- **CocoaPods** - iOS/macOS pods
- **Swift** - Swift Package Manager

### Container & Infrastructure
- **Docker/OCI** - Container images (manifests, layers)
- **Helm** - Kubernetes charts (.tgz)
- **Terraform** - Terraform modules and providers

### Linux Packages
- **Alpine** - APK packages (.apk)
- **Debian/Ubuntu** - DEB packages (.deb)
- **RPM/YUM** - RHEL/CentOS packages (.rpm)

### Other
- **AI/ML Models** - TensorFlow, PyTorch, ONNX

## Proxy Modes

cargobay acts as a transparent caching proxy for:

| Proxy | Path | Upstream |
|-------|------|----------|
| npm | `/npm` | registry.npmjs.org |
| Maven/Gradle/SBT | `/maven`, `/gradle`, `/sbt` | repo1.maven.org |
| Docker/OCI | `/v2` | registry-1.docker.io |
| PyPI | `/pypi` | pypi.org |
| NuGet | `/nuget` | api.nuget.org |
| Helm | `/helm` | registry-1.docker.io (charts) |
| Cargo | `/cargo` | crates.io |
| Go Modules | `/go` | proxy.golang.org |
| Alpine | `/alpine` | alpinelinux.org |
| Debian | `/debian` | debian.org |
| RPM/YUM | `/rpm`, `/yum` | rpm.org |
| Conan | `/conan` | conan.io |
| CocoaPods | `/cocoapods` | CocoaPods.org |
| Swift | `/swift` | Swift Package Index |
| Dart | `/dart` | pub.dev |
| Terraform | `/terraform` | registry.terraform.io |
| Composer | `/composer` | packagist.org |
| Conda | `/conda` | anaconda.org |

## Features

### Proxy Support (21 Formats)
- **Package Managers**: npm, Maven/Gradle/SBT, PyPI, NuGet, Cargo, Go Modules, Composer, Conda, Dart, CocoaPods, Swift
- **Container**: Docker/OCI, Helm, Terraform
- **Linux**: Alpine, Debian, RPM/YUM
- **Other**: Conan (C/C++)

### Core Features
- Universal artifact repository with upload/download
- Caching proxy for all supported registries
- Vulnerability scanning (Trivy/Grype)
- Role-Based Access Control (5 roles: admin, operator, developer, viewer, auditor)
- Audit logging for all actions
- Full-text search with autocomplete
- Digital signature verification (PGP)
- Cross-region replication with multi-master support

### Developer Experience
- Embedded React UI (single binary, no separate frontend server)
- First-run setup wizard
- Self-service user profile management
- API key management with expiration
- Session-based authentication + HTTP Basic (Docker)
- Prometheus metrics at `/metrics`

## Architecture

For detailed architecture documentation including component breakdown, data flows, and deployment patterns, see [docs/architecture.md](docs/architecture.md).

### Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        cargobay Server                                      │
│                        (Go + React SPA)                                     │
│                        Port 4500 (default)                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                  HTTP Server (chi Router)                            │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌───────────┐ │   │
│  │  │   API Layer │  │  Proxy      │  │  Web UI     │  │ Metrics │ │   │
│  │  │  (/api/v1)  │  │  Handlers   │  │  (embedded) │  │ (/metrics)│ │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └───────────┘ │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │              Core Services (RBAC, Auth, Cache, Search)               │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │              Data Layer (PostgreSQL, Storage, Redis)                 │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Components

The React frontend is embedded into the Go binary at build time, so the whole
application — UI, API, and every registry proxy — is one container listening
on a single port.

```
cargobay/
├── backend/            Go API + registry proxies (21+ formats)
│   ├── cmd/server/     entrypoint (main.go)
│   ├── pkg/
│   │   ├── api/        REST API handlers (server.go)
│   │   ├── auth/       Authentication (bcrypt, session keys)
│   │   ├── rbac/       Role-based access control (5 roles)
│   │   ├── middleware/ Auth, rate limiting, metrics
│   │   ├── proxy/      21 protocol-specific proxies
│   │   ├── storage/    S3, GCS, Azure, Local adapters
│   │   ├── cache/      Redis caching
│   │   ├── database/   PostgreSQL data access
│   │   ├── vulnerability/ Trivy/Grype scanning
│   │   ├── searchindex/ Full-text search rebuild
│   │   └── replication/ Cross-region replication
│   └── pkg/webui/      embeds frontend/dist via go:embed
├── frontend/           React/Vite UI, built to static assets
│   └── src/
├── Dockerfile          multi-stage: frontend → backend → alpine
├── k8s/                Kubernetes manifests for production
├── docker-compose.yml  local dev stack
└── docs/               Documentation (architecture, configuration)
```

### Local runtime (docker-compose)

```
┌────────────┐
│  backend   │  UI + /api + all 21 proxy paths
│  (Go/chi)  │
│   :4500    │
└─────┬──────┘
      │
┌─────┴─────┐
│           │
┌─▼─────┐ ┌─▼──────────┐
│ Redis │ │ PostgreSQL │
│ :6379 │ │   :5432    │
└───────┘ └────────────┘
```

### Production (Kubernetes)

```
┌──────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Ingress                           │
└─────────────────────────┬────────────────────────────────────────────┘
                          │
    ┌─────────────────────┼─────────────────────┐
    │                     │                     │
┌───▼───┐           ┌───▼───┐           ┌───▼───┐
│ App   │           │ App   │           │ App   │
│ Pod   │           │ Pod   │           │ Pod   │
│       │           │       │           │       │
│ Go    │           │ Go    │           │ Go    │
│ Server│           │ Server│           │ Server│
└───┬───┘           └───┬───┘           └───┬───┘
    │                   │                   │
    └───────────────────┼───────────────────┘
                        │
           ┌────────────▼────────────┐
           │   Redis Cluster         │
           │   (Cache + Sessions)    │
           └─────────────────────────┘
                        │
           ┌────────────▼────────────┐
           │   Object Storage        │
           │   (S3/GCS/Azure)        │
           │   (Artifact Content)    │
           └─────────────────────────┘
                        │
           ┌────────────▼────────────┐
           │   PostgreSQL            │
           │   (Metadata)            │
           └─────────────────────────┘
```

## Quick Start

### Docker Compose (recommended for local dev)

```bash
docker compose up -d --build
```

- App (UI + API + all proxies): http://localhost:4500
- Health check: http://localhost:4500/health
- First-run setup: http://localhost:4500/setup (create admin user)

```bash
docker compose down        # stop
docker compose down -v     # stop and drop postgres/storage volumes
```

### Docker (single image)

```bash
# Build
docker build -t cargobay:latest .

# Run
docker run -p 4500:4500 \
  -e DATABASE_URL=postgres://localhost:5432/cargobay \
  -e REDIS_URL=redis://localhost:6379 \
  cargobay:latest
```

### Kubernetes

```bash
kubectl apply -f k8s/
```

## Configuration

See `docs/configuration.md` for full configuration options.

### Environment Variables

| Variable | Description |
|----------|-------------|
| `CONFIG_FILE` | Path to YAML config file |
| `DATABASE_URL` | PostgreSQL connection string |
| `REDIS_URL` | Redis connection string |
| `STORAGE_TYPE` | Storage backend (local, s3, gcs, azure) |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | AWS credentials |
| `TRIVY_CACHE_DIR` | Trivy vulnerability DB cache |

### YAML Configuration

```yaml
server:
  port: 4500
  host: "0.0.0.0"

database:
  type: postgres
  dsn: postgresql://localhost:5432/cargobay

cache:
  type: redis
  url: redis://localhost:6379

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
```

## Development

```bash
make build          # build backend binary + frontend static assets
make backend-run    # run the backend locally
make backend-test   # run backend tests
make frontend-dev   # run Vite dev server (proxies to :4500)
make docker-up      # build and run via docker-compose
```

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | Internal architecture, components, data flows |
| [Configuration](docs/configuration.md) | Configuration options and environment variables |
| [Roadmap](docs/roadmap.md) | Planned features and improvements |

## Roles and Permissions

| Role | Description | Permissions |
|------|-------------|-------------|
| `admin` | System administrator | Full access to all features |
| `operator` | Registry operator | Manage registries, trigger scans |
| `developer` | Developer | Upload, download, delete own artifacts |
| `viewer` | Read-only | View artifacts and registries |
| `auditor` | Audit only | View audit logs and vulnerability reports |

## Metrics

The `/metrics` endpoint exposes Prometheus metrics:

- `cargobay_requests_total` - Request counts by method/path/status
- `cargobay_request_duration_seconds` - Request duration histogram
- `cargobay_cache_hits_total` / `cargobay_cache_misses_total`
- `cargobay_memory_usage_bytes` - Current memory usage
- `cargobay_uptime_seconds` - Server uptime

## License

MIT

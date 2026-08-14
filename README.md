# cargobay - Universal Binary Repository Manager

cargobay is a stateless, horizontally scalable universal binary repository manager for storing, organizing, and distributing software artifacts.

## Supported Artifact Types

- **Container Images** (Docker/OCI)
- **Java/Maven Artifacts** (JAR, WAR, POM)
- **NPM Packages** (Node.js)
- **AI/ML Models** (TensorFlow, PyTorch, ONNX)
- **Helm Charts**
- **Python Packages** (PyPI/Wheels)
- **NuGet Packages** (.NET)
- **Terraform Modules**
- **Helm Charts**

## Proxy Modes

cargobay can act as a transparent caching proxy for:

| Proxy | Path | Upstream |
|-------|------|----------|
| npm | `/npm` | registry.npmjs.org |
| Maven | `/maven` | repo1.maven.org |
| Docker | `/docker` | registry-1.docker.io |

## Features

- Caching proxy for npm, Maven, Docker registries
- Universal artifact repository
- SHA-256 signature verification
- User management with RBAC
- Prometheus metrics (`/metrics`)
- Horizontal scaling via Kubernetes
- Stateless design for easy scaling

## Architecture

### Components

The repo is split into two independently deployable components, each with its own Dockerfile:

```
cargobay/
├── backend/            Go API + registry proxy (npm/Maven/Docker)
│   ├── cmd/server/     entrypoint
│   ├── pkg/            proxy, storage, cache, database, middleware
│   └── Dockerfile
├── frontend/           React/Vite UI, served by nginx
│   ├── src/
│   ├── nginx.conf      listens on :5174
│   └── Dockerfile
├── k8s/                Kubernetes manifests for production
└── docker-compose.yml  local multi-container dev stack
```

### Local runtime (docker-compose)

```
┌────────────┐      ┌────────────┐
│  frontend  │─────▶│  backend   │
│  (nginx)   │ /api │ (Go/chi)   │
│  :5174     │      │  :4500     │
└────────────┘      └─────┬──────┘
                           │
                 ┌─────────┴─────────┐
                 │                   │
           ┌─────▼─────┐      ┌──────▼─────┐
           │   Redis    │      │ PostgreSQL │
           │  (cache)   │      │ (metadata) │
           │   :6379    │      │   :5432    │
           └────────────┘      └────────────┘
```

In production (`k8s/`), the backend runs as multiple stateless replicas behind an ingress, with the same Redis/PostgreSQL dependencies plus object storage (S3/GCS/Azure) for artifact content:

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

Spins up the frontend, backend, Redis, and PostgreSQL together:

```bash
docker compose up -d --build
```

- Frontend: http://localhost:5174
- Backend API: http://localhost:4500 (health check at `/health`)

```bash
docker compose down        # stop
docker compose down -v     # stop and drop postgres/storage volumes
```

### Docker (backend only)

```bash
# Build
docker build -t cargobay-backend:latest ./backend

# Run
docker run -p 4500:4500 \
  -e REDIS_URL=redis://localhost:6379 \
  -e DATABASE_URL=postgres://localhost:5432/cargobay \
  cargobay-backend:latest
```

### Kubernetes

```bash
kubectl apply -f k8s/
```

## Configuration

See `backend/config.example.yaml` for full configuration options.

## Development

```bash
make build          # build backend binary + frontend static assets
make backend-run    # run the backend locally
make backend-test   # run backend tests
make frontend-dev   # run the frontend dev server
make docker-up      # build and run the full stack via docker-compose
```

## License

MIT

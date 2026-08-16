# cargobay Documentation

Welcome to cargobay, the Universal Binary Repository Manager.

## Overview

cargobay is a unified artifact registry that supports multiple package formats including Docker/OCI, npm, Maven, PyPI, NuGet, Helm, and more. It acts as a proxy cache for upstream registries while providing additional features like vulnerability scanning, access control, and audit logging.

## Features

- **Multi-format Support**: Docker/OCI, npm, Maven, PyPI, NuGet, Helm, and generic artifacts
- **Proxy Caching**: Proxy and cache packages from upstream registries
- **Vulnerability Scanning**: Automatic security scanning of uploaded artifacts
- **Role-Based Access Control**: Fine-grained permissions with 5 roles
- **Audit Logging**: Complete audit trail of all actions
- **Multi-Storage Backends**: S3, GCS, Azure Blob Storage, and local storage
- **Kubernetes Ready**: Helm charts for easy deployment

## Quick Start

### Prerequisites

- Docker and Docker Compose
- 4GB+ RAM
- 20GB+ disk space

### Installation

```bash
# Clone the repository
git clone https://github.com/your-org/cargobay.git
cd cargobay

# Start with Docker Compose
docker compose up -d

# Access the UI
open http://localhost:3000
```

### Default Credentials

- Username: `admin`
- Password: `admin123`

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        cargobay                             │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │  Proxy      │  │  Storage    │  │  Redis      │         │
│  │  Handlers   │  │  Adapters   │  │  Cache      │         │
│  │  (npm,      │  │  (S3, GCS,  │  │             │         │
│  │   Maven,    │  │   Azure,    │  │             │         │
│  │   Docker,   │  │   Local)    │  │             │         │
│  │   PyPI,     │  │             │  │             │         │
│  │   NuGet)    │  │             │  │             │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │  PostgreSQL │  │  Elasticsearch│ │  Vulnerability│        │
│  │  Database   │  │  Search     │  │  Scanner    │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
└─────────────────────────────────────────────────────────────┘
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CB_PORT` | Server port | `8080` |
| `CB_HOST` | Server host | `localhost` |
| `CB_DATABASE_URL` | PostgreSQL connection string | `postgresql://localhost:5432/cargobay` |
| `CB_REDIS_URL` | Redis connection string | `redis://localhost:6379` |
| `CB_STORAGE_TYPE` | Storage backend | `local` |
| `CB_STORAGE_PATH` | Local storage path | `/var/lib/cargobay/storage` |
| `CB_CORS_ORIGINS` | Allowed CORS origins | `*` |
| `CB_LOG_LEVEL` | Log level | `info` |

### Storage Backends

#### S3

```yaml
storage:
  type: s3
  region: us-east-1
  bucket: cargobay-artifacts
  access_key_id: YOUR_ACCESS_KEY
  secret_access_key: YOUR_SECRET_KEY
```

#### GCS

```yaml
storage:
  type: gcs
  bucket: cargobay-artifacts
  credentials_file: /path/to/credentials.json
```

#### Azure

```yaml
storage:
  type: azure
  account_name: YOUR_ACCOUNT_NAME
  account_key: YOUR_ACCOUNT_KEY
  container: cargobay-artifacts
```

## API Reference

### Authentication

All API endpoints require authentication via Bearer token or session cookie.

```bash
# Login to get a session
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "admin123"}'

# Use the session cookie in subsequent requests
curl -X GET http://localhost:8080/api/v1/artifacts \
  -H "Cookie: session=YOUR_SESSION_ID"
```

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/health` | Health check |
| `GET` | `/api/v1/version` | Get server version |
| `GET` | `/api/v1/artifacts` | List artifacts |
| `GET` | `/api/v1/artifacts/:id` | Get artifact details |
| `POST` | `/api/v1/artifacts` | Upload artifact |
| `DELETE` | `/api/v1/artifacts/:id` | Delete artifact |
| `GET` | `/api/v1/vulnerabilities` | List vulnerabilities |
| `GET` | `/api/v1/audit-logs` | Get audit logs |
| `GET` | `/api/v1/metrics` | Prometheus metrics |

See the [API Reference](api.md) for detailed endpoint documentation.

## Roles and Permissions

| Role | Description | Permissions |
|------|-------------|-------------|
| `admin` | System administrator | Full access to all features |
| `operator` | Registry operator | Manage registries, trigger scans |
| `developer` | Developer | Upload, download, delete own artifacts |
| `viewer` | Read-only | View artifacts and registries |
| `auditor` | Audit only | View audit logs and vulnerability reports |

## Deployment

### Kubernetes

```bash
# Using Helm
helm install cargobay ./charts/cargobay \
  --set postgresql.enabled=true \
  --set redis.enabled=true \
  --set storage.type=s3 \
  --set storage.s3.bucket=my-bucket

# Or with values file
helm install cargobay ./charts/cargobay -f values.yaml
```

### Docker Compose

```yaml
version: '3.8'
services:
  cargobay:
    image: cargobay:latest
    ports:
      - "8080:8080"
      - "3000:3000"
    environment:
      - CB_DATABASE_URL=postgresql://postgres:5432/cargobay
      - CB_REDIS_URL=redis://redis:6379
    volumes:
      - cargobay-storage:/var/lib/cargobay/storage
    depends_on:
      - postgres
      - redis

  postgres:
    image: postgres:15
    environment:
      - POSTGRES_PASSWORD=secret
    volumes:
      - postgres-data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine

volumes:
  cargobay-storage:
  postgres-data:
```

## Troubleshooting

### Common Issues

**Connection refused to database**
- Ensure PostgreSQL is running and accessible
- Check database credentials in environment variables

**Storage write permissions**
- Verify the storage directory is writable by the cargobay user
- For S3/GCS/Azure, check credentials and bucket permissions

**High memory usage**
- Increase container memory limits
- Reduce Redis cache size via `CB_REDIS_MAX_MEMORY`

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

## License

This project is licensed under the MIT License - see the [LICENSE](../LICENSE) file for details.

## Support

- GitHub Issues: [https://github.com/your-org/cargobay/issues](https://github.com/your-org/cargobay/issues)
- Documentation: [https://docs.cargobay.io](https://docs.cargobay.io)
- Slack: [Join our community](https://slack.cargobay.io)

# Cargobay - Enterprise Artifact Registry

A universal artifact registry supporting millions of users, built for enterprises with petabyte-scale storage, high availability, and comprehensive security.

## Features

### 📦 Universal Artifact Support
- **Docker/OCI** - Container images
- **NPM** - Node.js packages
- **Maven/Gradle** - Java artifacts
- **PyPI** - Python packages
- **NuGet** - .NET packages
- **Helm/Chart** - Kubernetes charts
- **Generic** - Any file format

### 🌍 Distributed Storage
- S3-compatible storage (AWS, MinIO, etc.)
- Google Cloud Storage
- Azure Blob Storage
- Local storage fallback
- Intelligent tiering based on access patterns
- Cross-region replication

### 🔒 Enterprise Security
- **RBAC** - Role-based access control with 15+ permissions and 5+ roles
- **JWT Authentication** - Secure token-based auth
- **Audit Logging** - Complete audit trail
- **Artifact Signing** - PGP signature verification
- **Access Keys** - API key management with expiration

### 🚀 High Performance
- **Multi-tier Caching**:
  - Redis with configurable TTLs
  - Artifact cache: 24 hours
  - Registry cache: 5 minutes
  - Search cache: 15 minutes
- **Search**: Elasticsearch with PostgreSQL fallback
- **Connection Pooling**: Optimized database connections

### 🔄 Cross-Region Replication
- Peer-to-peer and hub-spoke models
- Async replication workers
- Automatic failover
- Consistency checks

### 🛡️ Vulnerability Scanning
- **Trivy** - Container scanning
- **Grype** - Go-based vulnerability scanner
- **npm-audit** - NPM package scanning
- **dependency-check** - Maven/Java scanning
- **safety** - Python package scanning

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Load Balancer                         │
└──────────────────────┬──────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────┐
        │              │              │
┌───────▼───────┐ ┌───▼───────┐ ┌───▼───────┐
│   API Node 1  │ │API Node 2 │ │API Node 3 │
└───────┬───────┘ └───┬───────┘ └───┬───────┘
        │             │             │
        └─────────────┼─────────────┘
                      │
        ┌─────────────┴─────────────┐
        │      Redis Cluster        │
        │   (Multi-tier Cache)      │
        └─────────────┬─────────────┘
                      │
        ┌─────────────┴─────────────┐
        │    PostgreSQL Cluster     │
        │  (Primary Database)       │
        └─────────────┬─────────────┘
                      │
        ┌─────────────┴─────────────┐
        │    Elasticsearch Cluster  │
        │       (Search)            │
        └─────────────┬─────────────┘
                      │
        ┌─────────────┴─────────────┐
        │  Storage Backends         │
        │  - S3 / GCS / Azure       │
        │  - Local Fallback         │
        └───────────────────────────┘
```

## Quick Start

### Prerequisites
- Go 1.21+
- PostgreSQL 14+
- Redis 7+
- Elasticsearch 8+

### Installation

```bash
# Clone the repository
git clone https://github.com/your-org/cargobay.git
cd cargobay/backend

# Install dependencies
go mod tidy

# Run migrations
go run cmd/migrate/main.go

# Start the server
go run cmd/server/main.go

# Start the enterprise API server (with full RBAC and REST API)
go run cmd/cli/main.go server --port 8080
```

### Configuration

```yaml
server:
  host: 0.0.0.0
  port: 8080

database:
  host: localhost
  port: 5432
  name: cargobay
  user: cargobay
  password: ${DB_PASSWORD}
  pool:
    max: 25
    min: 5
    max_lifetime: 30m

redis:
  address: localhost:6379
  database: 0
  artifact_ttl: 24h
  registry_ttl: 5m
  search_ttl: 15m

storage:
  primary: s3
  backends:
    s3:
      endpoint: https://s3.amazonaws.com
      region: us-east-1
      bucket: cargobay-artifacts
    local:
      path: /var/cargobay/storage

replication:
  enabled: true
  regions:
    - us-east-1
    - us-west-2
    - eu-west-1
  strategy: hub_spoke
  hub: us-east-1
```

## CLI Usage

```bash
# List artifacts
cargobay artifact list

# Upload artifact
cargobay artifact upload ./package.tar.gz --namespace library --name nginx --version 1.21.0

# Get artifact details
cargobay artifact get <artifact-id>

# Delete artifact
cargobay artifact delete <artifact-id>

# Scan for vulnerabilities
cargobay artifact scan <artifact-id>

# Sign an artifact
cargobay artifact sign <artifact-id> --key-id 0x12345678

# Manage users
cargobay user list
cargobay user get <user-id>
cargobay user create --username alice --email alice@example.com
cargobay user delete <user-id>
cargobay user roles <user-id>
cargobay user permissions <user-id>

# Manage roles
cargobay role list
cargobay role get <role-id>
cargobay role create --name custom-role
cargobay role delete <role-id>
cargobay role permissions <role-id>

# Manage registries
cargobay registry list
cargobay registry get <registry-id>
cargobay registry add --name npm --url https://registry.npmjs.org --type npm
cargobay registry delete <registry-id>

# View replication status
cargobay replication status

# Trigger replication sync
cargobay replication sync --region us-west-2

# List replication regions
cargobay replication regions
```

## API Reference

### Health Check
```
GET /health
```

### Artifacts
```
GET    /api/v1/artifacts          # List artifacts
POST   /api/v1/artifacts          # Create artifact
GET    /api/v1/artifacts/{id}     # Get artifact
DELETE /api/v1/artifacts/{id}     # Delete artifact
POST   /api/v1/artifacts/{id}/scan # Scan for vulnerabilities
```

### Search
```
GET /api/v1/search?q=query&limit=10&offset=0
```

### Registries
```
GET    /api/v1/registries
POST   /api/v1/registries
DELETE /api/v1/registries/{id}
```

### Users & RBAC
```
GET /api/v1/users
GET /api/v1/roles
GET /api/v1/permissions
GET /api/v1/users/{id}/roles
GET /api/v1/users/{id}/permissions
```

### Replication
```
GET /api/v1/replication/status
POST /api/v1/replication/sync
```

## Testing

```bash
# Run all tests
./run_tests.sh

# Run specific test package
go test ./pkg/storage/... -v
go test ./pkg/rbac/... -v

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## License

MIT License - See LICENSE file for details.

## Contributing

Contributions are welcome! Please read our contributing guidelines first.

## Support

For enterprise support, contact us at enterprise@cargobay.io

# cargobay Storage Requirements

This document clarifies the storage requirements for cargobay, a universal binary repository manager.

---

## Architecture Overview

cargobay is **stateless** and horizontally scalable. Storage is divided into two distinct categories:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  cargobay-1     │     │  cargobay-2     │     │  cargobay-N     │
│  (stateless)    │     │  (stateless)    │     │  (stateless)    │
└────────┬────────┘     └────────┬────────┘     └────────┬────────┘
         │                       │                       │
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
               ┌─────────────────┼─────────────────┐
               │                 │                 │
         ┌─────▼─────┐     ┌─────▼─────┐     ┌─────▼─────┐
         │  Redis    │     │  Postgres │     │  Storage  │
         │  (cache)  │     │  (metadata)│    │  (artifacts)│
         └───────────┘     └───────────┘     └───────────┘
```

---

## 1. Artifact Storage (Storage Adapter)

**Purpose:** Stores the actual binary artifacts (npm packages, Docker images, AI models, etc.)

### Supported Backends

| Backend | Local Volume | NFS | S3/GCS/Azure |
|---------|--------------|-----|--------------|
| **Local** | ✓ | ✗ | ✗ |
| **NFS** | ✗ | ✓ | ✗ |
| **S3** | ✗ | ✗ | ✓ |
| **GCS** | ✗ | ✗ | ✓ |
| **Azure** | ✗ | ✗ | ✓ |

### Configuration

```yaml
# Local storage with local volume
storage:
  type: local
  config:
    path: /path/to/storage

# NFS storage (mount NFS then use local adapter)
storage:
  type: local
  config:
    path: /mnt/nfs/artifacts

# S3 storage
storage:
  type: s3
  config:
    bucket: cargobay-artifacts
    region: us-east-1
    endpoint: https://s3.amazonaws.com  # optional, for custom endpoints
    access_key: YOUR_ACCESS_KEY
    secret_key: YOUR_SECRET_KEY

# GCS storage
storage:
  type: gcs
  config:
    bucket: cargobay-artifacts
    json_key: /path/to/service-account.json  # optional, uses default credentials if omitted

# Azure Blob Storage
storage:
  type: azure
  config:
    container: cargobay-artifacts
    account_url: https://account.blob.core.windows.net
    connection_string: <connection-string>  # or use other auth methods
```

### Storage Paths

Artifacts are stored with this structure:
```
<registry_id>/<namespace>/<artifact_name>/<version>/artifact.bin
```

Example:
```
dockerhub/library/nginx/1.25.1/artifact.bin
npm/@scope/package/1.0.0/artifact.bin
```

---

## 2. Database Storage (PostgreSQL)

**Purpose:** Stores metadata about artifacts (user info, registry config, audit logs, etc.)

### Supported Backends

| Backend | Local Volume | NFS | External Database |
|---------|--------------|-----|-------------------|
| **PostgreSQL** | ✓ | ⚠️ | ✓ |

### Important Notes

- **NFS is NOT recommended** for PostgreSQL data directory due to filesystem locking and fsync semantics that can cause corruption
- PostgreSQL should use **local disk or block storage** (EBS, PD, etc.)
- For production, consider managed PostgreSQL services (RDS, Cloud SQL, Azure Database)

### Local/Block Storage Recommendation

```yaml
database:
  type: postgres
  dsn: postgresql://user:password@localhost:5432/cargobay
  # Or with local volume:
  # dsn: postgresql://user:password@/cargobay?host=/var/run/postgresql
```

### External Database (Production)

```yaml
database:
  type: postgres
  dsn: postgresql://user:password@cluster.amazonaws.com:5432/cargobay
  # Or any external PostgreSQL endpoint
```

---

## 3. Redis Cache (Optional but Recommended)

**Purpose:** Caches artifact metadata and search results for performance

### Supported Backends

| Backend | Local | External |
|---------|-------|----------|
| **Redis** | ✓ | ✓ |

### Configuration

```yaml
cache:
  type: redis
  url: redis://localhost:6379
  ttl: 3600  # 1 hour
  max_size: 1073741824  # 1GB
```

### Deployment Options

1. **Local Redis** - For development/single-server deployments
2. **External Redis** - For production (ElastiCache, Redis Cloud, etc.)

---

## Minimal Deployment (Single Server)

**All-in-one setup using local volumes:**

```
/home/hwong/cargobay/
├── data/
│   ├── postgres/           # PostgreSQL data (local volume)
│   └── artifacts/          # Artifact storage (local volume)
├── config.yaml             # Configuration
└── docker-compose.yml      # Or run directly
```

**config.yaml:**
```yaml
server:
  port: 4500

storage:
  type: local
  config:
    path: ./data/artifacts

database:
  type: postgres
  dsn: postgresql://cargobay:password@localhost:5432/cargobay

cache:
  type: redis
  url: redis://localhost:6379
```

---

## Production Deployment Options

### Option A: All External (No NFS)
| Component | Backend |
|-----------|---------|
| Artifacts | S3/GCS/Azure |
| Database | RDS/Cloud SQL/Azure Database |
| Cache | ElastiCache/Redis Cloud |

### Option B: Hybrid (Local/Block + External Storage)
| Component | Backend |
|-----------|---------|
| Artifacts | S3/GCS (for durability/scalability) |
| Database | Local PostgreSQL on SSD/NVMe |
| Cache | Redis on same server |

### Option C: NFS for Artifacts (Not Recommended for Database)
| Component | Backend |
|-----------|---------|
| Artifacts | NFS (mounted, then `type: local` in config) |
| Database | Local PostgreSQL |
| Cache | Redis |

---

## Storage Sizing Guidelines

### Artifact Storage
- Small packages (npm, maven): 1KB - 10MB
- Docker images: 10MB - 5GB
- AI/ML models: 100MB - 50GB+

**Recommendation:** Start with 100GB, scale as needed. Use S3/GCS for unlimited scalability.

### Database Storage (PostgreSQL)
| Table | Estimated Size (per 10K artifacts) |
|-------|-----------------------------------|
| artifacts | ~10-50MB |
| users | ~1MB |
| access_keys | ~1MB |
| audit_log | ~100KB per 1K operations |

**Recommendation:** Start with 10GB for PostgreSQL.

### Redis Cache
**Recommendation:** 1-4GB depending on artifact count and search patterns.

---

## Migration from Local to S3/GCS

1. **Backup artifacts:**
```bash
tar -czf artifacts-backup.tar.gz /path/to/local/storage
```

2. **Upload to cloud storage:**
```bash
# S3 example
aws s3 sync ./data/artifacts s3://cargobay-artifacts/

# GCS example
gsutil -m rsync -r ./data/artifacts gs://cargobay-artifacts/
```

3. **Update config.yaml:**
```yaml
storage:
  type: s3  # or gcs
  config:
    bucket: cargobay-artifacts
    region: us-east-1
```

4. **Restart cargobay**

---

## Summary

| Requirement | Local/NFS Option | Cloud Option |
|-------------|------------------|--------------|
| **Artifacts** | Local volume OR NFS mount | S3/GCS/Azure Blob |
| **Database** | Local volume (SSD/NVMe recommended) | External PostgreSQL |
| **Cache** | Local Redis | External Redis |

**Key Takeaway:** 
- Use **S3/GCS/Azure** for artifact storage (scalable, durable)
- Use **local/block storage** for PostgreSQL (NFS not recommended)
- Use **Redis** (local or external) for caching

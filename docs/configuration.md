# Configuration Guide

This guide explains how to configure cargobay for your environment.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CB_PORT` | Server port | `8080` |
| `CB_HOST` | Server host | `localhost` |
| `CB_UI_PORT` | UI port | `3000` |
| `CB_DATABASE_URL` | PostgreSQL connection string | `postgresql://localhost:5432/cargobay` |
| `CB_REDIS_URL` | Redis connection string | `redis://localhost:6379` |
| `CB_STORAGE_TYPE` | Storage backend (local, s3, gcs, azure) | `local` |
| `CB_STORAGE_PATH` | Local storage path | `/var/lib/cargobay/storage` |
| `CB_CORS_ORIGINS` | Comma-separated list of allowed CORS origins | `*` |
| `CB_LOG_LEVEL` | Log level (debug, info, warn, error) | `info` |
| `CB_SESSION_SECRET` | Secret for session encryption | Auto-generated |
| `CB_ENCRYPTION_KEY` | Key for sensitive data encryption | Auto-generated |
| `CB_SCAN_ENABLED` | Enable vulnerability scanning | `true` |
| `CB_SCAN_INTERVAL` | Default scan interval in seconds | `86400` (24h) |
| `TRIVY_CACHE_DIR` | Trivy vulnerability-DB cache directory used by the backend's own DB-refresh scheduler; must match the volume mounted into the `trivy` server container | `/app/trivy-cache` |
| `CB_CACHE_TTL` | Cache TTL in seconds | `3600` (1h) |
| `CB_MAX_UPLOAD_SIZE` | Maximum upload size in bytes | `1073741824` (1GB) |

## Configuration Files

### YAML Configuration

Create a `config.yaml` file:

```yaml
server:
  port: 8080
  host: localhost

database:
  url: postgresql://localhost:5432/cargobay
  max_connections: 10
  ssl_mode: disable

redis:
  url: redis://localhost:6379
  max_idle: 10

storage:
  type: local
  path: /var/lib/cargobay/storage
  # For S3:
  # type: s3
  # region: us-east-1
  # bucket: cargobay-artifacts
  # access_key_id: YOUR_KEY
  # secret_access_key: YOUR_SECRET

security:
  session_secret: your-secret-key
  encryption_key: your-encryption-key
  cors_origins:
    - http://localhost:3000
    - https://example.com

scanning:
  enabled: true
  interval: 86400
```

## Storage Backend Configuration

### Local Storage

```yaml
storage:
  type: local
  path: /var/lib/cargobay/storage
```

### Amazon S3

```yaml
storage:
  type: s3
  region: us-east-1
  bucket: cargobay-artifacts
  access_key_id: YOUR_ACCESS_KEY
  secret_access_key: YOUR_SECRET_KEY
  # Optional:
  # endpoint: https://s3.us-east-1.amazonaws.com
  # force_path_style: false
```

### Google Cloud Storage

```yaml
storage:
  type: gcs
  bucket: cargobay-artifacts
  # Either credentials_file or credentials_json:
  credentials_file: /path/to/credentials.json
  # credentials_json: '{"type": "service_account", ...}'
```

### Azure Blob Storage

```yaml
storage:
  type: azure
  account_name: YOUR_ACCOUNT_NAME
  account_key: YOUR_ACCOUNT_KEY
  container: cargobay-artifacts
```

## Database Configuration

### PostgreSQL

```yaml
database:
  url: postgresql://user:password@host:5432/database
  max_connections: 20
  min_connections: 5
  max_idle_time: 300
```

### Connection String Format

```
postgresql://[user[:password]@][host][:port]/[database][?param1=value1&...]
```

## Redis Configuration

```yaml
redis:
  url: redis://user:password@host:6379
  max_idle: 10
  max_active: 20
  idle_timeout: 300
```

## Security Configuration

### Session Settings

```yaml
security:
  session_secret: your-secret-at-least-32-chars
  session_timeout: 86400  # 24 hours
  session_secure: true    # Use only with HTTPS
```

### CORS Configuration

```yaml
security:
  cors_origins:
    - http://localhost:3000
    - https://your-domain.com
  cors_methods:
    - GET
    - POST
    - PUT
    - DELETE
  cors_headers:
    - Content-Type
    - Authorization
```

### Rate Limiting

```yaml
security:
  rate_limit:
    enabled: true
    requests_per_minute: 60
    burst_size: 10
```

## Scanning Configuration

### Default Settings

```yaml
scanning:
  enabled: true
  interval: 86400  # 24 hours
  fail_on_critical: false
  ignore_pending: true
```

### Vulnerability DB Updates

Unlike the rest of this section, auto-update enablement and refresh
interval for Trivy's vulnerability database are **not** set via
`config.yaml` — they're stored in the `vulnerability_db_settings` table
and tunable live from Settings → Vulnerability DB in the UI, or via the
[Vulnerability DB Settings API](api.md#vulnerability-db-settings). This is
because the backend owns DB refreshes itself (shelling out to a bundled
`trivy` CLI) rather than relying on `trivy server`'s own updater, which
exposes no runtime control over its cadence. See
[Testing](contributing.md#testing) for how this scheduler is covered by
tests.

### Vulnerability Thresholds

```yaml
scanning:
  thresholds:
    critical: fail
    high: warn
    medium: ignore
    low: ignore
```

## Logging Configuration

### Log Levels

```yaml
logging:
  level: info  # debug, info, warn, error
  format: json  # json or text
  output: stdout  # stdout or file
  file_path: /var/log/cargobay/cargobay.log
```

## Helm Configuration

### values.yaml

```yaml
# Database
postgresql:
  enabled: true
  auth:
    username: cargobay
    password: secret
    database: cargobay

# Redis
redis:
  enabled: true
  auth:
    password: secret

# Storage
storage:
  type: s3
  s3:
    region: us-east-1
    bucket: cargobay-artifacts
    accessKey: YOUR_KEY
    secretKey: YOUR_SECRET

# Server
server:
  port: 8080
  replicas: 3
  resources:
    limits:
      cpu: 2
      memory: 4Gi
    requests:
      cpu: 1
      memory: 2Gi

# Ingress
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: cargobay.example.com
      paths:
        - path: /
          pathType: Prefix
```

## Environment-Specific Configurations

### Development

```bash
export CB_LOG_LEVEL=debug
export CB_SCAN_ENABLED=false
export CB_CACHE_TTL=60
```

### Production

```bash
export CB_LOG_LEVEL=warn
export CB_SCAN_ENABLED=true
export CB_CACHE_TTL=3600
export CB_SESSION_SECRET=$(openssl rand -hex 32)
export CB_ENCRYPTION_KEY=$(openssl rand -hex 32)
```

### Testing

```bash
export CB_DATABASE_URL=postgresql://test:test@localhost:5432/cargobay_test
export CB_STORAGE_TYPE=local
export CB_STORAGE_PATH=/tmp/cargobay_test
```

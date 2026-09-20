# Configuration Guide

This guide explains how to configure cargobay for your environment.

## Quick Reference

| Setting | Environment Variable | YAML Key | Description |
|---------|---------------------|----------|-------------|
| Server Port | `PORT` | `server.port` | Server port (fixed to 4500) |
| Server Host | `HOST` | `server.host` | Server host | 0.0.0.0 |
| Database | `DATABASE_URL` | `database.dsn` | PostgreSQL connection string |
| Redis | `REDIS_URL` | `cache.url` | Redis connection string |
| Storage Type | `STORAGE_TYPE` | `storage.type` | Storage backend |
| Trivy Cache | `TRIVY_CACHE_DIR` | - | Trivy DB cache directory |

## Environment Variables

## Environment Variables

Only the variables below are actually read by `config.LoadConfig()`
(`backend/pkg/config/config.go`); anything else must go through
`config.yaml` instead.

| Variable | Description | Default |
|----------|-------------|---------|
| `CONFIG_FILE` | Path to the YAML config file to load | `./config.yaml` |
| `PORT` | Server port — **note:** currently only checked for presence; setting it always resets the port to the built-in default of `4500` rather than using the value you pass | `4500` |
| `HOST` | Server host | `0.0.0.0` |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://localhost:5432/cargobay` |
| `REDIS_URL` | Redis connection string | `redis://localhost:6379` |
| `STORAGE_TYPE` | Storage backend (local, s3, gcs, azure) | `local` |
| `S3_ACCESS_KEY` | S3 access key (only used when `STORAGE_TYPE=s3`) | none |
| `S3_SECRET_KEY` | S3 secret key (only used when `STORAGE_TYPE=s3`) | none |
| `TRIVY_CACHE_DIR` | Trivy vulnerability-DB cache directory used by the backend's own DB-refresh scheduler; must match the volume mounted into the `trivy` server container | `/app/trivy-cache` |

The frontend is built to static assets and embedded into the backend binary
at build time (`backend/pkg/webui`), so it's served from the same host and
`PORT` as the API — there is no separate frontend port or process.

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

## Docker Registry Mirror

### Configuring the upstream

All upstream registries are configured under `registries:` in `config.yaml`,
not via environment variables. cargobay supports 21 package formats:

| Type | Description | Upstream URL |
|------|-------------|--------------|
| `docker` | Docker/OCI images | https://registry-1.docker.io |
| `npm` | Node.js packages | https://registry.npmjs.org |
| `maven` | Java/Maven artifacts | https://repo1.maven.org |
| `gradle` | Gradle dependencies (Maven-compatible) | https://repo1.maven.org |
| `sbt` | SBT dependencies (Maven-compatible) | https://repo1.maven.org |
| `pypi` | Python packages | https://pypi.org |
| `nuget` | .NET packages | https://api.nuget.org |
| `helm` | Kubernetes Helm charts | https://registry-1.docker.io |
| `cargo` | Rust crates | https://crates.io |
| `go` | Go modules | https://proxy.golang.org |
| `alpine` | Alpine Linux packages | https://alpinelinux.org |
| `debian` | Debian/Ubuntu packages | https://deb.debian.org |
| `rpm` | RPM packages | https://rpm.org |
| `yum` | YUM packages | https://yum.org |
| `conan` | Conan C++ packages | https://conan.io |
| `cocoapods` | iOS/macOS pods | https://cocoapods.org |
| `swift` | Swift packages | https://swiftpackageindex.com |
| `dart` | Dart packages | https://pub.dev |
| `terraform` | Terraform modules | https://registry.terraform.io |
| `composer` | PHP packages | https://packagist.org |
| `conda` | Conda packages | https://repo.anaconda.com |

### Example Configuration

```yaml
registries:
  - id: dockerhub
    name: Docker Hub
    type: docker
    url: https://registry-1.docker.io
    proxy: true
    enabled: true
    priority: 1

  - id: npmjs
    name: npm Registry
    type: npm
    url: https://registry.npmjs.org
    proxy: true
    enabled: true
    priority: 2

  - id: maven-central
    name: Maven Central
    type: maven
    url: https://repo1.maven.org
    proxy: true
    enabled: true
    priority: 2
```

**Use `url` for the upstream address, not `upstream`.** `RegistryConfig` in
`backend/pkg/config/config.go` has an `upstream` YAML field, but
`main.go`'s conversion from `cfg.Registries` to the runtime
`database.RegistryConfig` only copies `url` — `upstream` is silently
dropped. Setting `upstream:` in `config.yaml` looks valid and loads without
error, but the proxy pulls nothing through, because `url` (the field it
actually reads) is empty. This is a known bug in the current field mapping;
until it's fixed, `url` is the only field that works.

### Pointing a Docker client at the mirror

The Docker proxy is mounted under `/v2`, matching where Docker/OCI clients
always request `/v2/...` at the registry host — they don't accept a path
prefix, so this must stay `/v2` and not `/docker`. Configure your Docker
daemon's `registry-mirrors` with cargobay's bare host and port, with **no
path suffix**:

```json
{
  "registry-mirrors": ["https://cargobay.example.com:4500"]
}
```

If cargobay is served over plain HTTP, add the same host to
`insecure-registries` as well. Restart the Docker daemon after editing
`daemon.json`.

### Private/multiple docker registries

To expose more than one docker upstream (or a private one reachable only
under its own hostname), bind a registry to a specific host via its `Host`
field on `database.RegistryConfig` — this isn't exposed in the
`registries:` YAML list (which has no `Host` field), so it must be set
through the registries API or the Settings UI after the registry exists.
Requests whose `Host` header matches a bound registry are routed to it
directly; anything else falls back to the first enabled public proxy
registry of that type. See [Registries](api.md#registries) in the API
reference.

### Reaching a non-mirrored upstream: the `dkr/` path prefix

`registry-mirrors` only ever intercepts pulls from Docker Hub — a plain
`docker pull ghcr.io/org/image` (or quay.io, gcr.io, etc.) goes straight to
that upstream and never touches cargobay, no matter what's in
`daemon.json`. Host binding (above) solves this, but needs its own
DNS/hostname and TLS cert per upstream.

As an alternative, the Docker proxy also recognizes a `dkr/` marker as the
first path segment of a request, addressing any configured docker registry
by path instead of by hostname — no extra DNS needed, just one cargobay
hostname for everything:

```
docker pull cargobay.example.com/dkr/<registry-selector>/<repo>:<tag>
```

`<registry-selector>` matches a registry either by its `id` or by the
hostname of its configured `url` — the exact `url` on that registry's
config, not a "known" hostname for the provider. Docker Hub, for instance,
is normally configured with `url: https://registry-1.docker.io` (its real
v2 API endpoint), not `docker.io`, so the selector for it is
`registry-1.docker.io`. For example, with a registry configured as:

```yaml
registries:
  - id: ghcr
    name: GitHub Container Registry
    type: docker
    url: https://ghcr.io
    proxy: true
    enabled: true
    priority: 3
```

either of these reach it:

```
docker pull cargobay.example.com/dkr/ghcr.io/org/image:tag   # by url hostname
docker pull cargobay.example.com/dkr/ghcr/org/image:tag      # by registry id
```

The matched segment (`dkr/ghcr.io/` or `dkr/ghcr/`) is stripped before the
remaining path is parsed as the repository/reference, so the upstream
receives the plain `org/image:tag` reference it expects. A Host header bound
to a specific registry still takes priority over `dkr/` matching, and a
request path that doesn't start with `dkr/` is never treated as
path-prefix addressing — it always resolves via Host binding or the default
registry, exactly as before. This means an unprefixed pull, e.g.
`docker pull cargobay.example.com/nginx:latest`, always resolves to
whichever docker registry is configured as default (see priority above) —
so if you drop `registry-mirrors` entirely and rely only on `dkr/`
addressing, remember to prefix every pull explicitly, including Docker Hub
ones: `docker pull cargobay.example.com/dkr/registry-1.docker.io/library/nginx:latest`
(note official images need the `library/` namespace).

Matching by `id` (but not by upstream hostname) also works for a **private,
non-proxy** registry — one you push your own images to rather than mirror
from somewhere else (`proxy: false`; see
[Private/multiple docker registries](#privatemultiple-docker-registries)
above). This is the only path-based way to reach that kind of registry,
since it has no upstream URL to match a hostname against. For example, with
a private registry configured as `id: tinkertown`, `proxy: false`, a user
granted publish access can push without needing a dedicated hostname:

```
docker login cargobay.example.com
docker tag myimage cargobay.example.com/dkr/tinkertown/myimage:latest
docker push cargobay.example.com/dkr/tinkertown/myimage:latest
```

## npm, Maven, and PyPI Registries

Like Docker, the npm, Maven, and PyPI proxies (Maven's implementation is
shared by Gradle and SBT, since all three consume standard Maven-layout
repositories) support publishing to a private registry, not just pulling
from one. There is no `dkr/`-style path-prefix addressing for these
registries — a private registry must be bound to a `Host` (see
[Private Docker registries](#privatemultiple-docker-registries) for the
general mechanism; it applies the same way regardless of registry `type`),
and clients publish to it via that hostname, same as pulls. The publishing
user needs a `CanPublish` grant on the registry — per-user via the registry
access API/UI, or via a [group](api.md#groups) — same requirement as
`docker push`.

### Publishing to npm

```ini
# .npmrc
registry=https://npm.cargobay.example.com/npm/
//npm.cargobay.example.com/npm/:_authToken=<personal-access-token>
```

```
npm publish
```

Scoped packages (`@myscope/mypackage`) work the same way — no extra
configuration beyond the registry/token above.

### Publishing to Maven (and Gradle/SBT)

```xml
<!-- settings.xml -->
<servers>
  <server>
    <id>cargobay</id>
    <username>your-username</username>
    <password>your-personal-access-token</password>
  </server>
</servers>
```

```xml
<!-- pom.xml -->
<distributionManagement>
  <repository>
    <id>cargobay</id>
    <url>https://maven.cargobay.example.com/maven/</url>
  </repository>
</distributionManagement>
```

```
mvn deploy
```

or, without editing `pom.xml`:

```
mvn deploy -DaltDeploymentRepository=cargobay::default::https://maven.cargobay.example.com/maven/<group>/<artifact>/<version>/
```

Gradle and SBT publish to the same repository layout — point their
publish-repository configuration at `https://gradle.cargobay.example.com/gradle/`
or `https://sbt.cargobay.example.com/sbt/` respectively, using the same
credentials.

### Publishing to PyPI

```ini
# ~/.pypirc
[distutils]
index-servers = cargobay

[cargobay]
repository = https://pypi.cargobay.example.com/legacy/
username = __token__
password = <personal-access-token>
```

```
twine upload --repository cargobay dist/*
```

Unlike npm (PUT to a per-package URL) and Maven (PUT to a per-file URL),
PyPI's legacy upload API is a single `POST` of a `multipart/form-data` body
carrying the package name, version, and file as form fields — twine's
`repository`/`repository-url` setting can point at either the registry root
or its `/legacy/` alias, both work identically.

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

## Architecture Overview

For a detailed understanding of how cargobay's components interact, see the [Architecture Documentation](architecture.md). This covers:

- **Component breakdown**: API layer, proxy handlers, data layer
- **Data flows**: Artifact upload, Docker pull, search operations
- **Deployment patterns**: Development (Docker Compose) and production (Kubernetes)
- **Security model**: Authentication, authorization, data protection

# API Reference

This document describes the cargobay REST API.

## Base URL

```
http://localhost:8080/api/v1
```

## Authentication

All endpoints require authentication. You can authenticate using:

1. **Session Cookie** - After logging in via `/login`
2. **Bearer Token** - Pass the token in the `Authorization` header

```bash
# Using session cookie (after login)
curl -X GET http://localhost:8080/api/v1/artifacts

# Using bearer token
curl -X GET http://localhost:8080/api/v1/artifacts \
  -H "Authorization: Bearer YOUR_TOKEN"
```

## Endpoints

### Health & Version

#### Health Check

```http
GET /health
```

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-02-15T14:30:00Z"
}
```

#### Get Version

```http
GET /version
```

**Response:**
```json
{
  "version": "1.0.0",
  "build": "abc123",
  "timestamp": "2024-02-15T10:00:00Z"
}
```

### Artifacts

#### List Artifacts

```http
GET /artifacts
```

**Query Parameters:**
- `page` - Page number (default: 1)
- `limit` - Items per page (default: 20)
- `registry_id` - Filter by registry
- `type` - Filter by artifact type (docker, npm, maven, pypi, nuget, helm, generic)
- `search` - Search query
- `sort` - Sort field (uploaded_at, downloads, name)

**Response:**
```json
{
  "artifacts": [
    {
      "id": "abc123",
      "name": "nginx",
      "version": "1.25.3",
      "type": "docker",
      "namespace": "library",
      "registry_id": "dockerhub",
      "size": 145000000,
      "downloads": 1000000,
      "uploaded_at": "2024-02-15T10:30:00Z",
      "tags": ["latest", "stable"]
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 1,
    "total_pages": 1
  }
}
```

#### Get Artifact Details

```http
GET /artifacts/:id
```

**Response:**
```json
{
  "id": "abc123",
  "name": "nginx",
  "version": "1.25.3",
  "type": "docker",
  "namespace": "library",
  "registry_id": "dockerhub",
  "size": 145000000,
  "downloads": 1000000,
  "description": "Official nginx image",
  "tags": ["latest", "stable"],
  "uploaded_by": "john.doe",
  "uploaded_at": "2024-02-15T10:30:00Z",
  "metadata": {
    "sha256": "abc123...",
    "manifest": {...}
  }
}
```

#### Upload Artifact

```http
POST /artifacts
Content-Type: multipart/form-data
```

**Form Data:**
- `file` - The artifact file (required)
- `registry_id` - Target registry (required)
- `artifact_type` - Type of artifact (docker, npm, maven, pypi, nuget, helm, generic)
- `namespace` - Namespace (optional)
- `artifact_name` - Name of the artifact
- `version` - Version string
- `tags` - JSON array of tags
- `description` - Description

**Response:**
```json
{
  "id": "abc123",
  "message": "Artifact uploaded successfully",
  "artifact": {
    "id": "abc123",
    "name": "nginx",
    "version": "1.25.3",
    "type": "docker",
    "namespace": "library",
    "registry_id": "dockerhub",
    "size": 145000000,
    "downloads": 0,
    "uploaded_at": "2024-02-15T14:30:00Z",
    "tags": []
  }
}
```

#### Delete Artifact

```http
DELETE /artifacts/:id
```

**Response:**
```json
{
  "message": "Artifact deleted successfully"
}
```

### Vulnerabilities

#### List Vulnerabilities

```http
GET /vulnerabilities
```

**Query Parameters:**
- `page` - Page number
- `limit` - Items per page
- `severity` - Filter by severity (critical, high, medium, low)
- `status` - Filter by status (new, reviewed, mitigated, ignored)
- `artifact_id` - Filter by artifact

**Response:**
```json
{
  "vulnerabilities": [
    {
      "id": "vuln123",
      "artifact_id": "abc123",
      "artifact_name": "nginx",
      "artifact_version": "1.25.3",
      "type": "docker",
      "severity": "critical",
      "cve": "CVE-2024-24576",
      "description": "Memory leak in HTTP/3",
      "score": 9.1,
      "vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/C:N/I:N/A:H",
      "status": "new",
      "first_detected": "2024-02-15T14:30:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 1
  }
}
```

#### Get Vulnerability Details

```http
GET /vulnerabilities/:id
```

#### Scan Artifact

```http
POST /artifacts/:id/scan
```

### Audit Logs

#### List Audit Logs

```http
GET /audit-logs
```

**Query Parameters:**
- `page` - Page number
- `limit` - Items per page
- `category` - Filter by category (artifact, user, registry, system, security)
- `action` - Filter by action
- `result` - Filter by result (success, failure)
- `user` - Filter by username

**Response:**
```json
{
  "logs": [
    {
      "id": "log123",
      "timestamp": "2024-02-15T14:30:00Z",
      "action": "artifact.upload",
      "category": "artifact",
      "user": "john.doe",
      "email": "john@example.com",
      "ip": "192.168.1.100",
      "details": {
        "artifact_name": "nginx",
        "version": "1.25.3"
      },
      "result": "success"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 1
  }
}
```

### Registries

#### List Registries

```http
GET /registries
```

**Response:**
```json
{
  "registries": [
    {
      "id": "dockerhub",
      "name": "Docker Hub",
      "type": "docker",
      "url": "https://registry.hub.docker.com",
      "status": "active",
      "last_sync": "2024-02-15T14:30:00Z",
      "artifact_count": 12500,
      "total_size": 450000000000
    }
  ]
}
```

#### Get Registry Details

```http
GET /registries/:id
```

#### Sync Registry

```http
POST /registries/:id/sync
```

### Users & Roles

#### List Users

```http
GET /users
```

**Response:**
```json
{
  "users": [
    {
      "id": "user123",
      "username": "john.doe",
      "email": "john@example.com",
      "role": "developer",
      "last_login": "2024-02-15T10:00:00Z",
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

#### Create User

```http
POST /users
Content-Type: application/json
```

**Request Body:**
```json
{
  "username": "new.user",
  "email": "new@example.com",
  "password": "securepassword",
  "role": "developer"
}
```

#### Update User

```http
PUT /users/:id
Content-Type: application/json
```

#### Delete User

```http
DELETE /users/:id
```

### Vulnerability DB Settings

Controls how the backend refreshes Trivy's vulnerability database. The
backend bundles the `trivy` CLI and shells out to it on a schedule against
the same cache directory the `trivy` scan server uses — `trivy server` mode
itself exposes no HTTP control over its own update cadence, so cargobay
owns refreshes instead. Requires `system:read` (GET) / `system:write`
(PUT, POST) permissions, held by the `admin` role.

#### Get Settings

```http
GET /settings/vulnerability-db
```

**Response:**
```json
{
  "autoUpdateEnabled": true,
  "updateIntervalHours": 24,
  "lastCheckedAt": "2024-02-15T14:30:00Z",
  "lastUpdatedAt": "2024-02-15T14:30:00Z",
  "lastError": ""
}
```

#### Update Settings

```http
PUT /settings/vulnerability-db
Content-Type: application/json
```

**Request Body:**
```json
{
  "autoUpdateEnabled": true,
  "updateIntervalHours": 12
}
```

`updateIntervalHours` must be at least 1. Returns the updated settings row
(same shape as GET). Only the toggle and interval are writable here — the
status fields (`lastCheckedAt`, `lastUpdatedAt`, `lastError`) are stamped
exclusively by the update process itself.

#### Trigger Update Now

```http
POST /settings/vulnerability-db/update
```

Synchronously runs `trivy --cache-dir <dir> image --download-db-only` and
returns the resulting settings row. A download failure is reported via the
`lastError` field, not as an HTTP error status — the request itself
succeeded in attempting the update. Responds `503` if the updater was not
configured at startup.

### Metrics

#### Prometheus Metrics

```http
GET /metrics
```

Returns metrics in Prometheus format for scraping by Prometheus.

## Error Responses

### 400 Bad Request

```json
{
  "error": "invalid_request",
  "message": "Missing required field: artifact_name"
}
```

### 401 Unauthorized

```json
{
  "error": "unauthorized",
  "message": "Authentication required"
}
```

### 403 Forbidden

```json
{
  "error": "forbidden",
  "message": "Insufficient permissions"
}
```

### 404 Not Found

```json
{
  "error": "not_found",
  "message": "Artifact not found"
}
```

### 500 Internal Server Error

```json
{
  "error": "internal_error",
  "message": "An unexpected error occurred"
}
```

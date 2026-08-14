// Package config provides configuration management for cargobay
//
// Configuration is loaded from environment variables and optional YAML file.
// The following environment variables are supported:
//
//   PORT           - Server port (default: 4500)
//   HOST           - Server host (default: 0.0.0.0)
//   REDIS_URL      - Redis connection string
//   DATABASE_URL   - PostgreSQL connection string
//   STORAGE_TYPE   - Storage backend: s3, nfs, local, gcs, azure
//
// YAML Configuration File:
//
//   server:
//     port: 4500
//     host: 0.0.0.0
//   storage:
//     type: s3
//     config:
//       bucket: my-bucket
//       region: us-east-1
//   database:
//     type: postgres
//     dsn: postgres://localhost:5432/cargobay
//   cache:
//     type: redis
//     url: redis://localhost:6379
//     ttl: 1h
//   registries:
//     - id: dockerhub
//       name: Docker Hub
//       url: https://registry-1.docker.io
//       type: docker
//       proxy: true
package config

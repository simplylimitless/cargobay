// Package database provides PostgreSQL data access for cargobay
//
// This package handles all database operations including:
//   - Artifact metadata storage and retrieval
//   - User management and authentication
//   - Access key management
//   - Registry configuration
//
// Supported operations:
//   - GetArtifact - Retrieve artifact by identifier
//   - GetArtifactByDigest - Content-addressable lookup
//   - ListArtifacts - Pagination with filtering
//   - SearchArtifacts - Full-text search
//   - SaveArtifact - Upsert with conflict resolution
//   - CreateUser - Create user with roles
//   - CreateAccessKey - Generate API keys
package database

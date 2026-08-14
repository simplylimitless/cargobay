// Package storage provides artifact content storage adapters
//
// Supported backends:
//   - S3Adapter      - AWS S3
//   - GCSAdapter     - Google Cloud Storage
//   - NFSAdapter     - Network File System
//   - AzureAdapter   - Azure Blob Storage
//   - LocalAdapter   - Local file system
//
// Key features:
//   - Content-addressable storage
//   - Artifact integrity verification
//   - Concurrent upload/download
//   - Path-based organization
package storage

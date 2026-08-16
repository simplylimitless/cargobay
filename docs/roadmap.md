# Roadmap

This document outlines the planned features and improvements for cargobay.

## Completed Features

### Core Functionality
- [x] Backend REST API with chi router
- [x] Proxy handlers: npm, Maven, Docker
- [x] RBAC with 5 roles (admin, operator, developer, viewer, auditor)
- [x] Storage adapters: S3, GCS, Azure, Local
- [x] Redis caching
- [x] PostgreSQL database with full-text search
- [x] Vulnerability scanner
- [x] Prometheus metrics

### Additional Features
- [x] PyPI Proxy (PEP 503)
- [x] NuGet Proxy (V2/V3)
- [x] CLI Tool
- [x] Elasticsearch Integration
- [x] Helm Chart
- [x] E2E Tests
- [x] CI/CD Pipeline
- [x] Documentation Site

## Current Status

**v1.0.0** - Complete and ready for deployment

All core features implemented and tested. Production-ready with Kubernetes support.

## Planned Enhancements

### Short-term (Next 1-2 months)

#### 1. Artifact Signing
- [ ] Full PGP signature verification
- [ ] Notary support
- [ ] Signature policy enforcement

#### 2. Admin Dashboard
- [ ] Enhanced UI for system monitoring
- [ ] Real-time metrics visualization
- [ ] System configuration UI

#### 3. Enhanced Search
- [ ] Advanced search filters
- [ ] Faceted search
- [ ] Search result highlighting

### Medium-term (3-6 months)

#### 1. Advanced Replication
- [ ] Cross-region sync with conflict resolution
- [ ] Multi-master replication support
- [ ] Geographic distribution optimization

#### 2. Security Enhancements
- [ ] SSO integration (SAML, OAuth2, OpenID Connect)
- [ ] Secret scanning integration
- [ ] Vulnerability auto-patching

#### 3. AI/ML Model Support
- [ ] Model registry endpoints
- [ ] Model versioning and lineage
- [ ] Model deployment integration

### Long-term (6+ months)

#### 1. Edge Caching
- [ ] CDN integration
- [ ] Global edge cache network
- [ ] Offline mode support

#### 2. Advanced Search
- [ ] Semantic search integration
- [ ] AI-powered artifact recommendations
- [ ] Custom search facets

#### 3. Multi-tenancy
- [ ] Organization-level isolation
- [ ] Resource quotas
- [ ] Tenant-specific configurations

## Technical Debt & Improvements

- [ ] Migration system (golang-migrate or similar)
- [ ] API versioning strategy
- [ ] Breaking change management
- [ ] Performance profiling and optimization
- [ ] Memory leak detection and fixes

## Known Limitations

- Docker manifest list support is limited to v2 schema
- Large file uploads (>1GB) may require proxy configuration
- Elasticsearch integration requires manual setup

## Migration Path

### From v0.x to v1.0

1. Backup your database
2. Update to latest v0.x version
3. Run database migrations
4. Deploy v1.0
5. Verify all features

### Breaking Changes

- None in v1.0 (initial stable release)

## Feature Requests

We welcome feature requests! Please open an issue on GitHub with:

1. A clear description of the feature
2. Use case and benefits
3. Any relevant technical details

## Contributing

Contributions are welcome! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

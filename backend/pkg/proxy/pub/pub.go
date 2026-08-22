// Package pub implements the Dart/Flutter pub package repository API.
//
// This package provides a caching proxy for pub packages that:
//   - Serves the package-versions listing (api/packages/{name})
//   - Caches .tar.gz archive downloads from an upstream pub server
//
// Pub repository API: https://github.com/dart-lang/pub/blob/master/doc/repository-spec-v2.md
package pub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// PubProxy implements the pub package repository API.
type PubProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewPubProxy creates a new Dart/Flutter pub proxy instance.
func NewPubProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &PubProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "dart"))

	r.Get("/api/packages/{name}", proxy.handlePackageInfo)
	r.Get("/api/packages/{name}/versions/{version}.tar.gz", proxy.handleArchive)

	return r
}

type pubVersion struct {
	Version    string                 `json:"version"`
	ArchiveURL string                 `json:"archive_url"`
	Pubspec    map[string]interface{} `json:"pubspec"`
}

// handlePackageInfo serves the api/packages/{name} listing, pointing
// archive_url at our own archive endpoint.
func (p *PubProxy) handlePackageInfo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	t := proxypkg.TargetFromContext(r, "dart")

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{ArtifactType: "dart"})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list versions: %v", err), http.StatusInternalServerError)
		return
	}

	var versions []pubVersion
	for _, a := range artifacts {
		if a.ArtifactName != name {
			continue
		}
		versions = append(versions, pubVersion{
			Version:    a.Version,
			ArchiveURL: fmt.Sprintf("%s/api/packages/%s/versions/%s.tar.gz", baseURL(r), name, a.Version),
			Pubspec:    map[string]interface{}{"name": name, "version": a.Version},
		})
	}

	if len(versions) == 0 {
		info, err := p.fetchInfoFromUpstream(t.Reg, name)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch package info: %v", err), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
		return
	}

	latest := versions[len(versions)-1]
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":     name,
		"latest":   latest,
		"versions": versions,
	})
}

// fetchInfoFromUpstream fetches and records upstream package version
// metadata, without rewriting archive_url — used only to discover which
// versions exist; downloads still route through handleArchive.
func (p *PubProxy) fetchInfoFromUpstream(reg *database.RegistryConfig, name string) (map[string]interface{}, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	url := fmt.Sprintf("%s/api/packages/%s", strings.TrimSuffix(reg.URL, "/"), name)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(req, reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return info, nil
}

// handleArchive handles .tar.gz archive downloads.
func (p *PubProxy) handleArchive(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	t := proxypkg.TargetFromContext(r, "dart")

	if data, err := p.storage.GetArtifact("dart", "", name, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/gzip")
		if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
			fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
		}
		w.Write(data)
		return
	}

	upstreamPath := fmt.Sprintf("api/packages/%s/versions/%s.tar.gz", name, version)
	data, err := p.fetchFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch archive: %v", err), http.StatusBadGateway)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("dart:%s:%s:%s", t.Label, name, version),
		RegistryID:   t.Label,
		ArtifactType: "dart",
		ArtifactName: name,
		Version:      version,
		Size:         int64(len(data)),
		Metadata:     map[string]interface{}{},
		Tags:         []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save package metadata: %v\n", err)
	}
	if _, err := p.storage.SaveArtifact("dart", "", name, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save archive: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
		fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
	}
	w.Write(data)
}

func (p *PubProxy) fetchFromUpstream(reg *database.RegistryConfig, path string) ([]byte, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	url := fmt.Sprintf("%s/%s", strings.TrimSuffix(reg.URL, "/"), path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(req, reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

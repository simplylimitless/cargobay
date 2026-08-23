// Package cocoapods implements a caching CocoaPods Specs-repo proxy.
//
// This mirrors the trunk CDN layout (cdn.cocoapods.org) that CocoaPods'
// CDN-backed source uses: an MD5-sharded Specs/ tree of per-version
// podspec.json files, plus an all_pods.txt name listing. CocoaPods itself
// resolves each pod's actual source (git tag, tarball, etc.) from URLs
// inside the podspec — this proxy only caches the specs, the same
// metadata-only layer package managers with external source URLs use.
//
// CDN layout: https://github.com/CocoaPods/cdn.cocoapods.org
package cocoapods

import (
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

// CocoaPodsProxy implements the CocoaPods Specs-repo CDN protocol.
type CocoaPodsProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// NewCocoaPodsProxy creates a new CocoaPods proxy instance.
func NewCocoaPodsProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &CocoaPodsProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "cocoapods"))

	r.Get("/all_pods.txt", proxy.handleAllPods)
	r.Get("/Specs/{s1}/{s2}/{s3}/{name}/{version}/{filename}.podspec.json", proxy.handlePodspec)

	return r
}

// handleAllPods serves the flat list of known pod names.
func (p *CocoaPodsProxy) handleAllPods(w http.ResponseWriter, r *http.Request) {
	t := proxypkg.TargetFromContext(r, "cocoapods")
	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{ArtifactType: "cocoapods"})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list pods: %v", err), http.StatusInternalServerError)
		return
	}

	seen := map[string]bool{}
	var names []string
	for _, a := range artifacts {
		if !seen[a.ArtifactName] {
			seen[a.ArtifactName] = true
			names = append(names, a.ArtifactName)
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(strings.Join(names, "\n") + "\n"))
}

// handlePodspec serves (or fetches-and-caches) a single podspec.json.
func (p *CocoaPodsProxy) handlePodspec(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	version := chi.URLParam(r, "version")
	s1, s2, s3 := chi.URLParam(r, "s1"), chi.URLParam(r, "s2"), chi.URLParam(r, "s3")

	t := proxypkg.TargetFromContext(r, "cocoapods")

	if data, err := p.storage.GetArtifact(t.Label, "", name, version); err == nil && data != nil {
		w.Header().Set("Content-Type", "application/json")
		if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
			fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
		}
		w.Write(data)
		return
	}

	upstreamPath := fmt.Sprintf("Specs/%s/%s/%s/%s/%s/%s.podspec.json", s1, s2, s3, name, version, name)
	data, err := p.fetchFromUpstream(t.Reg, upstreamPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch podspec: %v", err), http.StatusBadGateway)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("cocoapods:%s:%s:%s", t.Label, name, version),
		RegistryID:   t.Label,
		ArtifactType: "cocoapods",
		ArtifactName: name,
		Version:      version,
		Size:         int64(len(data)),
		Metadata:     map[string]interface{}{},
		Tags:         []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save podspec metadata: %v\n", err)
	}
	if _, err := p.storage.SaveArtifact(t.Label, "", name, version, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save podspec: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := p.db.IncrementArtifactDownloads(t.Label, "", name, version); err != nil {
		fmt.Printf("failed to record download for %s@%s: %v\n", name, version, err)
	}
	w.Write(data)
}

func (p *CocoaPodsProxy) fetchFromUpstream(reg *database.RegistryConfig, path string) ([]byte, error) {
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

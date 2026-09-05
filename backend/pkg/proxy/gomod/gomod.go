// Package gomod implements the Go module proxy protocol (GOPROXY).
//
// This package provides a caching proxy for Go modules that:
//   - Serves @v/list, @v/{version}.info, @v/{version}.mod, @v/{version}.zip
//   - Serves @latest
//   - Caches modules from an upstream Go module proxy (proxy.golang.org by
//     default)
//
// GOPROXY protocol: https://go.dev/ref/mod#goproxy-protocol
package gomod

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	proxypkg "github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/go-chi/chi/v5"
)

// GoModProxy implements the Go module proxy protocol.
type GoModProxy struct {
	db      *database.Database
	storage storage.StorageAdapter
	cache   *cache.Cache
}

// versionInfo is the JSON body of @v/{version}.info and @latest.
type versionInfo struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

// NewGoModProxy creates a new Go module proxy instance.
func NewGoModProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &GoModProxy{db: db, storage: storage, cache: cache}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "go"))

	// Module paths contain slashes and are variable-depth, so this is
	// handled with a single catch-all route parsed manually, rather than
	// chi path params.
	r.Get("/*", proxy.handleRequest)

	return r
}

// handleRequest dispatches based on the well-known suffix of the request
// path, per the GOPROXY protocol.
func (p *GoModProxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	switch {
	case strings.HasSuffix(path, "/@v/list"):
		module := unescapeModulePath(strings.TrimSuffix(path, "/@v/list"))
		p.handleList(w, r, module)
	case strings.HasSuffix(path, "/@latest"):
		module := unescapeModulePath(strings.TrimSuffix(path, "/@latest"))
		p.handleLatest(w, r, module)
	case strings.HasSuffix(path, ".info"), strings.HasSuffix(path, ".mod"), strings.HasSuffix(path, ".zip"):
		p.handleVersioned(w, r, path)
	default:
		http.NotFound(w, r)
	}
}

// handleVersioned handles @v/{version}.info, .mod, and .zip.
func (p *GoModProxy) handleVersioned(w http.ResponseWriter, r *http.Request, path string) {
	idx := strings.Index(path, "/@v/")
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	module := unescapeModulePath(path[:idx])
	rest := path[idx+len("/@v/"):]
	ext := rest[strings.LastIndex(rest, "."):]
	version := unescapeModulePath(strings.TrimSuffix(rest, ext))

	t := proxypkg.TargetFromContext(r, "go")

	switch ext {
	case ".info":
		artifact, err := p.db.GetArtifactByParams("go", "", module, version)
		if err != nil || artifact == nil {
			info, err := p.fetchInfoFromUpstream(t.Reg, t.Label, module, version)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to fetch module info: %v", err), http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(info)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(versionInfo{Version: artifact.Version, Time: artifact.Created})
	case ".mod":
		if data, err := p.storage.GetArtifact(t.Label, "", module, version); err == nil && data != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.Write(data)
			return
		}
		data, err := p.fetchModFromUpstream(t.Reg, module, version)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch go.mod: %v", err), http.StatusBadGateway)
			return
		}
		p.storage.SaveArtifact(t.Label, "", module, version, data)
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
	case ".zip":
		if rc, err := p.storage.GetArtifactStream(t.Label, "", module, version); err == nil && rc != nil {
			defer rc.Close()
			w.Header().Set("Content-Type", "application/zip")
			if err := p.db.IncrementArtifactDownloads(t.Label, "", module, version); err != nil {
				fmt.Printf("failed to record download for %s@%s: %v\n", module, version, err)
			}
			io.Copy(w, rc)
			return
		}
		resp, err := p.fetchZipStreamFromUpstream(t.Reg, module, version)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch module zip: %v", err), http.StatusBadGateway)
			return
		}
		counter := &proxypkg.CountingReader{R: resp.Body}
		_, err = p.storage.SaveArtifactStream(t.Label, "", module, version, counter)
		resp.Body.Close()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to save module: %v", err), http.StatusInternalServerError)
			return
		}
		rc, err := p.storage.GetArtifactStream(t.Label, "", module, version)
		if err != nil || rc == nil {
			http.Error(w, "Failed to serve module", http.StatusInternalServerError)
			return
		}
		defer rc.Close()
		w.Header().Set("Content-Type", "application/zip")
		if err := p.db.IncrementArtifactDownloads(t.Label, "", module, version); err != nil {
			fmt.Printf("failed to record download for %s@%s: %v\n", module, version, err)
		}
		io.Copy(w, rc)
	default:
		http.NotFound(w, r)
	}
}

// handleList serves @v/list — one known version per line.
func (p *GoModProxy) handleList(w http.ResponseWriter, r *http.Request, module string) {
	t := proxypkg.TargetFromContext(r, "go")
	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{ArtifactType: "go"})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list module versions: %v", err), http.StatusBadGateway)
		return
	}

	var versions []string
	for _, a := range artifacts {
		if a.ArtifactName == module {
			versions = append(versions, a.Version)
		}
	}
	sort.Strings(versions)

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(strings.Join(versions, "\n") + "\n"))
}

// handleLatest serves @latest.
func (p *GoModProxy) handleLatest(w http.ResponseWriter, r *http.Request, module string) {
	t := proxypkg.TargetFromContext(r, "go")

	info, err := p.fetchInfoFromUpstream(t.Reg, t.Label, module, "")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch latest version: %v", err), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// fetchInfoFromUpstream fetches @v/{version}.info (or @latest when version
// is empty) from the upstream Go module proxy and records it as a known
// artifact version.
func (p *GoModProxy) fetchInfoFromUpstream(reg *database.RegistryConfig, registryLabel, module, version string) (*versionInfo, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}

	suffix := "@latest"
	if version != "" {
		suffix = fmt.Sprintf("@v/%s.info", escapeModulePath(version))
	}
	infoURL := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(reg.URL, "/"), escapeModulePath(module), suffix)

	req, err := http.NewRequest(http.MethodGet, infoURL, nil)
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

	var info versionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	artifact := &database.ArtifactMetadata{
		ID:           fmt.Sprintf("go:%s:%s:%s", registryLabel, module, info.Version),
		RegistryID:   registryLabel,
		ArtifactType: "go",
		ArtifactName: module,
		Version:      info.Version,
		Created:      info.Time,
		Updated:      info.Time,
		Metadata:     map[string]interface{}{},
		Tags:         []string{info.Version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save module version metadata: %v\n", err)
	}

	return &info, nil
}

// fetchModFromUpstream fetches a module's go.mod file from upstream.
func (p *GoModProxy) fetchModFromUpstream(reg *database.RegistryConfig, module, version string) ([]byte, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	modURL := fmt.Sprintf("%s/%s/@v/%s.mod", strings.TrimSuffix(reg.URL, "/"), escapeModulePath(module), escapeModulePath(version))
	return fetchBytes(reg, modURL)
}

// fetchZipStreamFromUpstream fetches a module's source zip from upstream,
// leaving the response body open for streaming into storage.
func (p *GoModProxy) fetchZipStreamFromUpstream(reg *database.RegistryConfig, module, version string) (*http.Response, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	zipURL := fmt.Sprintf("%s/%s/@v/%s.zip", strings.TrimSuffix(reg.URL, "/"), escapeModulePath(module), escapeModulePath(version))
	req, err := http.NewRequest(http.MethodGet, zipURL, nil)
	if err != nil {
		return nil, err
	}
	proxypkg.ApplyUpstreamAuth(req, reg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	return resp, nil
}

func fetchBytes(reg *database.RegistryConfig, url string) ([]byte, error) {
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

// escapeModulePath applies Go's module-path escaping: every uppercase
// letter is replaced with "!" followed by its lowercase form, since module
// proxy URLs must be all lowercase (case-insensitive filesystems/URLs
// would otherwise collide github.com/User and github.com/user).
func escapeModulePath(path string) string {
	var b strings.Builder
	for _, c := range path {
		if c >= 'A' && c <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(c - 'A' + 'a')
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// unescapeModulePath reverses escapeModulePath.
func unescapeModulePath(escaped string) string {
	var b strings.Builder
	for i := 0; i < len(escaped); i++ {
		c := escaped[i]
		if c == '!' && i+1 < len(escaped) {
			b.WriteByte(escaped[i+1] - 'a' + 'A')
			i++
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

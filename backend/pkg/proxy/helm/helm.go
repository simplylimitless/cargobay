// Package helm implements the Helm Chart registry API proxy
//
// This package provides a caching proxy for Helm charts that:
//   - Implements Helm Chart Repository API
//   - Caches charts from registry-1.docker.io and other registries
//   - Handles .tgz chart files
//   - Supports index.yaml generation
//
// Helm Chart Repository API: https://helm.sh/docs/topics/chart_repository/
package helm

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
	"gopkg.in/yaml.v3"
)

// HelmProxy implements the Helm Chart Repository API proxy
type HelmProxy struct {
	db         *database.Database
	storage    storage.StorageAdapter
	cache      *cache.Cache
	registries []database.RegistryConfig
	registry   string
}

// ChartVersion represents a versioned chart entry in index.yaml
type ChartVersion struct {
	Name        string         `yaml:"name" json:"name"`
	Version     string         `yaml:"version" json:"version"`
	Description string         `yaml:"description" json:"description"`
	Keywords    []string       `yaml:"keywords" json:"keywords"`
	Maintainers []*Maintainer  `yaml:"maintainers" json:"maintainers"`
	Home        string         `yaml:"home" json:"home"`
	Sources     []string       `yaml:"sources" json:"sources"`
	Icon        string         `yaml:"icon" json:"icon"`
	Annotations map[string]string `yaml:"annotations" json:"annotations"`
	Digest      string         `yaml:"digest" json:"digest"`
	Urls        []string       `yaml:"urls" json:"urls"`
	Created     time.Time      `yaml:"created" json:"created"`
}

// Maintainer represents a chart maintainer
type Maintainer struct {
	Name  string `yaml:"name" json:"name"`
	Email string `yaml:"email" json:"email"`
	URL   string `yaml:"url" json:"url"`
}

// IndexFile represents a Helm chart repository index
type IndexFile struct {
	APIVersion string                     `yaml:"apiVersion" json:"apiVersion"`
	Generated  time.Time                  `yaml:"generated" json:"generated"`
	Entries    map[string][]*ChartVersion `yaml:"entries" json:"entries"`
}

// NewHelmProxy creates a new Helm proxy instance
func NewHelmProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig) chi.Router {
	r := chi.NewRouter()

	proxy := &HelmProxy{
		db:         db,
		storage:    storage,
		cache:      cache,
		registries: registries,
		registry:   "helm",
	}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, "helm"))

	// Helm Chart Repository API routes
	// Root: / - returns 200 OK
	r.Get("/", proxy.handleRoot)

	// Index.yaml: /index.yaml - returns chart repository index
	r.Get("/index.yaml", proxy.handleIndex)

	// Chart download: /charts/{name}-{version}.tgz
	r.Get("/charts/{chartName}-{version}.tgz", proxy.handleChartDownload)

	// Chart tarball: /{name}/{version}/{name}-{version}.tgz
	r.Get("/{chartName}/{version}/{chartNameFile}-{versionFile}.tgz", proxy.handleChartDownloadAlt)

	// Chart file: /{chartName}/{version}/{filename}
	r.Get("/{chartName}/{version}/{filename}", proxy.handleChartFile)

	// Charts directory
	r.Get("/charts/", proxy.handleChartsDir)
	r.Get("/charts", proxy.handleChartsDir)

	return r
}

// handleRoot handles the root endpoint
func (h *HelmProxy) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleIndex handles index.yaml generation
func (h *HelmProxy) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Try cache first
	if data, err := h.cacheGet("-helm-index-"); err == nil {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Header().Set("X-Content-Duration", "300") // 5 minutes
		w.Write(data)
		return
	}

	// Get all Helm charts from database
	artifacts, err := h.db.ListArtifacts(proxypkg.TargetFromContext(r, "helm").Label, database.ListOptions{
		ArtifactType: "helm",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list charts: %v", err), http.StatusInternalServerError)
		return
	}

	// Build entries map
	entries := make(map[string][]*ChartVersion)

	for _, a := range artifacts {
		meta, ok := a.Metadata["metadata"].(map[string]interface{})
		if !ok {
			meta = a.Metadata
		}

		chartName := a.ArtifactName
		if namespace, ok := meta["namespace"].(string); ok && namespace != "" {
			chartName = namespace + "/" + a.ArtifactName
		}

		maintainers := []*Maintainer{}
		if m, ok := meta["maintainers"].([]interface{}); ok {
			for _, mm := range m {
				if mmMap, ok := mm.(map[string]interface{}); ok {
					maintainers = append(maintainers, &Maintainer{
						Name:  mmMap["name"].(string),
						Email: mmMap["email"].(string),
						URL:   mmMap["url"].(string),
					})
				}
			}
		}

		sources := []string{}
		if s, ok := meta["sources"].([]interface{}); ok {
			for _, ss := range s {
				sources = append(sources, ss.(string))
			}
		}

		keywords := []string{}
		if k, ok := meta["keywords"].([]interface{}); ok {
			for _, kk := range k {
				keywords = append(keywords, kk.(string))
			}
		}

		annotations := map[string]string{}
		if ann, ok := meta["annotations"].(map[string]interface{}); ok {
			for k, v := range ann {
				annotations[k] = v.(string)
			}
		}

		description := ""
		if desc, ok := meta["description"].(string); ok {
			description = desc
		}

		home := ""
		if homeURL, ok := meta["home"].(string); ok {
			home = homeURL
		}

		icon := ""
		if iconURL, ok := meta["icon"].(string); ok {
			icon = iconURL
		}

		chartVer := &ChartVersion{
			Name:        a.ArtifactName,
			Version:     a.Version,
			Description: description,
			Maintainers: maintainers,
			Home:        home,
			Sources:     sources,
			Icon:        icon,
			Keywords:    keywords,
			Annotations: annotations,
			Digest:      a.Digest,
			Urls:        []string{fmt.Sprintf("/charts/%s-%s.tgz", a.ArtifactName, a.Version)},
			Created:     a.Created,
		}

		entries[chartName] = append(entries[chartName], chartVer)
	}

	// Sort versions for each chart
	for name := range entries {
		sort.Slice(entries[name], func(i, j int) bool {
			return entries[name][i].Version > entries[name][j].Version
		})
	}

	// Create index file
	index := &IndexFile{
		APIVersion: "v1",
		Generated:  time.Now(),
		Entries:    entries,
	}

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(index)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate index: %v", err), http.StatusInternalServerError)
		return
	}

	h.cacheSet("-helm-index-", yamlBytes)

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("X-Content-Duration", "300")
	w.Write(yamlBytes)
}

// handleChartDownload handles chart download
func (h *HelmProxy) handleChartDownload(w http.ResponseWriter, r *http.Request) {
	chartName := chi.URLParam(r, "chartName")
	version := chi.URLParam(r, "version")
	fileName := fmt.Sprintf("%s-%s.tgz", chartName, version)
	t := proxypkg.TargetFromContext(r, "helm")

	// Try to get from storage first
	if rc, err := h.storage.GetArtifactStream(t.Label, "", chartName, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
		if err := h.db.IncrementArtifactDownloads(t.Label, "", chartName, version); err != nil {
			fmt.Printf("failed to record download for %s %s: %v\n", chartName, version, err)
		}
		io.Copy(w, rc)
		return
	}

	// Fetch from upstream
	if err := h.fetchAndSaveChart(t.Reg, t.Label, chartName, version, fileName); err != nil {
		http.Error(w, fmt.Sprintf("Failed to download chart: %v", err), http.StatusBadGateway)
		return
	}

	rc, err := h.storage.GetArtifactStream(t.Label, "", chartName, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve chart", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/x-gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	if err := h.db.IncrementArtifactDownloads(t.Label, "", chartName, version); err != nil {
		fmt.Printf("failed to record download for %s %s: %v\n", chartName, version, err)
	}
	io.Copy(w, rc)
}

// handleChartDownloadAlt handles alternative chart download path
func (h *HelmProxy) handleChartDownloadAlt(w http.ResponseWriter, r *http.Request) {
	chartName := chi.URLParam(r, "chartName")
	version := chi.URLParam(r, "version")
	t := proxypkg.TargetFromContext(r, "helm")

	// Try to get from storage first
	if rc, err := h.storage.GetArtifactStream(t.Label, "", chartName, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/x-gzip")
		if err := h.db.IncrementArtifactDownloads(t.Label, "", chartName, version); err != nil {
			fmt.Printf("failed to record download for %s %s: %v\n", chartName, version, err)
		}
		io.Copy(w, rc)
		return
	}

	// Fetch from upstream
	fileName := fmt.Sprintf("%s-%s.tgz", chartName, version)
	if err := h.fetchAndSaveChart(t.Reg, t.Label, chartName, version, fileName); err != nil {
		http.Error(w, fmt.Sprintf("Failed to download chart: %v", err), http.StatusBadGateway)
		return
	}

	rc, err := h.storage.GetArtifactStream(t.Label, "", chartName, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve chart", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/x-gzip")
	if err := h.db.IncrementArtifactDownloads(t.Label, "", chartName, version); err != nil {
		fmt.Printf("failed to record download for %s %s: %v\n", chartName, version, err)
	}
	io.Copy(w, rc)
}

// handleChartFile handles chart file access (values.yaml, Chart.yaml, etc.)
func (h *HelmProxy) handleChartFile(w http.ResponseWriter, r *http.Request) {
	chartName := chi.URLParam(r, "chartName")
	version := chi.URLParam(r, "version")
	filename := chi.URLParam(r, "filename")

	// For now, return a placeholder response
	// In a full implementation, this would extract the chart from storage
	// and return the requested file

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(fmt.Sprintf("File %s not found in chart %s %s", filename, chartName, version)))
}

// handleChartsDir handles charts directory listing
func (h *HelmProxy) handleChartsDir(w http.ResponseWriter, r *http.Request) {
	// Get all Helm charts from database
	artifacts, err := h.db.ListArtifacts(proxypkg.TargetFromContext(r, "helm").Label, database.ListOptions{
		ArtifactType: "helm",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list charts: %v", err), http.StatusInternalServerError)
		return
	}

	// Build list of charts
	charts := make(map[string]*ChartInfo)
	for _, a := range artifacts {
		key := a.ArtifactName
		if _, exists := charts[key]; !exists {
			charts[key] = &ChartInfo{
				Name: a.ArtifactName,
			}
		}
		charts[key].Versions = append(charts[key].Versions, a.Version)
	}

	// Sort versions
	for _, chart := range charts {
		sort.Slice(chart.Versions, func(i, j int) bool {
			return chart.Versions[i] > chart.Versions[j]
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"charts": charts,
	})
}

// fetchAndSaveChart fetches a chart from upstream and streams it straight
// into storage, then saves its (minimal) metadata record. It does not
// buffer the whole chart in memory: unlike the old fetchChartFromUpstream,
// it also does not keep a copy in the in-process byte cache — caching a
// large chart tarball there would reintroduce the same full-blob memory
// buildup this streaming conversion is meant to eliminate, and the on-disk
// storage cache (checked by the caller before this is even called) already
// serves repeat requests.
func (h *HelmProxy) fetchAndSaveChart(reg *database.RegistryConfig, registryLabel, chartName, version, fileName string) error {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return fmt.Errorf("no upstream proxy configured for this registry")
	}
	upstream := reg.URL

	// Construct chart URL
	// Helm chart repositories typically organize charts like:
	// /charts/name-version.tgz
	chartURL := fmt.Sprintf("%s/charts/%s", strings.TrimSuffix(upstream, "/"), fileName)

	chartReq, err := http.NewRequest(http.MethodGet, chartURL, nil)
	if err != nil {
		return err
	}
	proxypkg.ApplyUpstreamAuth(chartReq, reg)
	resp, err := http.DefaultClient.Do(chartReq)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	counter := &proxypkg.CountingReader{R: resp.Body}
	if _, err := h.storage.SaveArtifactStream(registryLabel, "", chartName, version, counter); err != nil {
		resp.Body.Close()
		return err
	}
	resp.Body.Close()

	// Save a minimal metadata record. A full implementation would extract
	// the .tgz and parse Chart.yaml for richer metadata.
	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("helm:%s:%s:%s", registryLabel, chartName, version),
		RegistryID:      registryLabel,
		ArtifactType:    "helm",
		Namespace:       "",
		ArtifactName:    chartName,
		Version:         version,
		DigestAlgorithm: "sha256",
		Size:            counter.N,
		Created:         time.Now(),
		Updated:         time.Now(),
		Metadata: map[string]interface{}{
			"name":    chartName,
			"version": version,
			"type":    "helm",
		},
		Tags: []string{version},
	}
	if err := h.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save chart metadata: %v\n", err)
	}

	return nil
}

// ChartInfo represents chart information for directory listing
type ChartInfo struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
}

// cacheGet retrieves data from cache
func (h *HelmProxy) cacheGet(key string) ([]byte, error) {
	var data []byte
	err := h.cache.Get(key, &data)
	return data, err
}

// cacheSet stores data in cache
func (h *HelmProxy) cacheSet(key string, data []byte) {
	h.cache.Set(key, data)
}

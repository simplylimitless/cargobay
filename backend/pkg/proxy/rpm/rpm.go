// Package rpm implements a caching YUM/DNF (RPM) repository proxy.
//
// This package provides a caching proxy for RPM packages that:
//   - Generates repodata/repomd.xml and repodata/primary.xml.gz on demand
//     from cached artifact metadata
//   - Caches .rpm packages from an upstream repository
//
// The same implementation backs both the "rpm" and "yum" registry types —
// yum/dnf are just different clients speaking the identical repo protocol
// (repomd.xml + primary.xml + .rpm files), so NewRPMProxy is parameterized
// by artifactType rather than duplicated.
//
// YUM repository format: https://createrepo.baseurl.org/
package rpm

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
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

// RPMProxy implements a YUM/DNF repository proxy. Packages are namespaced
// by architecture (e.g. "x86_64", "noarch").
type RPMProxy struct {
	db           *database.Database
	storage      storage.StorageAdapter
	cache        *cache.Cache
	artifactType string
}

// NewRPMProxy creates a new RPM/YUM proxy instance. artifactType is either
// "rpm" or "yum" — both speak the same protocol against separately
// configured registries.
func NewRPMProxy(db *database.Database, storage storage.StorageAdapter, cache *cache.Cache, rbacMgr *rbac.RBAC, registries []database.RegistryConfig, artifactType string) chi.Router {
	r := chi.NewRouter()

	proxy := &RPMProxy{db: db, storage: storage, cache: cache, artifactType: artifactType}

	r.Use(proxypkg.RequireReadAccess(db, rbacMgr, registries, artifactType))

	r.Get("/{arch}/repodata/repomd.xml", proxy.handleRepomd)
	r.Get("/{arch}/repodata/primary.xml.gz", proxy.handlePrimary)
	r.Get("/{arch}/{fileName}", proxy.handlePackage)

	return r
}

type repomdData struct {
	Type     string `xml:"type,attr"`
	Location struct {
		Href string `xml:"href,attr"`
	} `xml:"location"`
}

type repomdXML struct {
	XMLName xml.Name     `xml:"repomd"`
	Xmlns   string       `xml:"xmlns,attr"`
	Data    []repomdData `xml:"data"`
}

// handleRepomd serves repodata/repomd.xml, pointing clients at
// primary.xml.gz.
func (p *RPMProxy) handleRepomd(w http.ResponseWriter, r *http.Request) {
	doc := repomdXML{
		Xmlns: "http://linux.duke.edu/metadata/repo",
		Data: []repomdData{{
			Type: "primary",
			Location: struct {
				Href string `xml:"href,attr"`
			}{Href: "repodata/primary.xml.gz"},
		}},
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(doc)
}

type primaryPackage struct {
	Type    string `xml:"type,attr"`
	Name    string `xml:"name"`
	Arch    string `xml:"arch"`
	Version struct {
		Ver string `xml:"ver,attr"`
	} `xml:"version"`
	Size struct {
		Package int64 `xml:"package,attr"`
	} `xml:"size"`
	Location struct {
		Href string `xml:"href,attr"`
	} `xml:"location"`
}

type primaryXML struct {
	XMLName  xml.Name         `xml:"metadata"`
	Xmlns    string           `xml:"xmlns,attr"`
	Packages []primaryPackage `xml:"package"`
}

// handlePrimary serves repodata/primary.xml.gz, the package listing.
func (p *RPMProxy) handlePrimary(w http.ResponseWriter, r *http.Request) {
	arch := chi.URLParam(r, "arch")
	t := proxypkg.TargetFromContext(r, p.artifactType)

	artifacts, err := p.db.ListArtifacts(t.Label, database.ListOptions{
		Namespace:    arch,
		ArtifactType: p.artifactType,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list packages: %v", err), http.StatusInternalServerError)
		return
	}

	doc := primaryXML{Xmlns: "http://linux.duke.edu/metadata/common"}
	for _, a := range artifacts {
		pkg := primaryPackage{Type: "rpm", Name: a.ArtifactName, Arch: a.Namespace}
		pkg.Version.Ver = a.Version
		pkg.Size.Package = a.Size
		pkg.Location.Href = fmt.Sprintf("%s-%s.%s.rpm", a.ArtifactName, a.Version, a.Namespace)
		doc.Packages = append(doc.Packages, pkg)
	}

	var xmlBuf bytes.Buffer
	xmlBuf.WriteString(xml.Header)
	xml.NewEncoder(&xmlBuf).Encode(doc)

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	gw.Write(xmlBuf.Bytes())
	gw.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Write(gzBuf.Bytes())
}

// handlePackage handles .rpm package downloads.
func (p *RPMProxy) handlePackage(w http.ResponseWriter, r *http.Request) {
	arch := chi.URLParam(r, "arch")
	fileName := chi.URLParam(r, "fileName")
	name, version, ok := splitRPMFileName(fileName, arch)
	if !ok {
		http.NotFound(w, r)
		return
	}

	t := proxypkg.TargetFromContext(r, p.artifactType)

	if rc, err := p.storage.GetArtifactStream(t.Label, arch, name, version); err == nil && rc != nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/x-rpm")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
		if err := p.db.IncrementArtifactDownloads(t.Label, arch, name, version); err != nil {
			fmt.Printf("failed to record download for %s-%s.%s: %v\n", name, version, arch, err)
		}
		io.Copy(w, rc)
		return
	}

	resp, err := p.fetchStreamFromUpstream(t.Reg, arch, fileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download package: %v", err), http.StatusBadGateway)
		return
	}
	counter := &proxypkg.CountingReader{R: resp.Body}
	_, err = p.storage.SaveArtifactStream(t.Label, arch, name, version, counter)
	resp.Body.Close()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save package: %v", err), http.StatusInternalServerError)
		return
	}

	artifact := &database.ArtifactMetadata{
		ID:              fmt.Sprintf("%s:%s:%s:%s:%s", p.artifactType, t.Label, arch, name, version),
		RegistryID:      t.Label,
		ArtifactType:    p.artifactType,
		Namespace:       arch,
		ArtifactName:    name,
		Version:         version,
		DigestAlgorithm: "sha256",
		Size:            counter.N,
		Metadata:        map[string]interface{}{},
		Tags:            []string{version},
	}
	if err := p.db.SaveArtifact(artifact); err != nil {
		fmt.Printf("Warning: failed to save package metadata: %v\n", err)
	}

	rc, err := p.storage.GetArtifactStream(t.Label, arch, name, version)
	if err != nil || rc == nil {
		http.Error(w, "Failed to serve package", http.StatusInternalServerError)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/x-rpm")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	if err := p.db.IncrementArtifactDownloads(t.Label, arch, name, version); err != nil {
		fmt.Printf("failed to record download for %s-%s.%s: %v\n", name, version, arch, err)
	}
	io.Copy(w, rc)
}

func (p *RPMProxy) fetchStreamFromUpstream(reg *database.RegistryConfig, arch, fileName string) (*http.Response, error) {
	if reg == nil || !reg.Proxy || reg.URL == "" {
		return nil, fmt.Errorf("no upstream proxy configured for this registry")
	}
	url := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(reg.URL, "/"), arch, fileName)
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

// splitRPMFileName splits "name-version.arch.rpm" into ("name", "version"),
// given the arch is already known from the URL path.
func splitRPMFileName(fileName, arch string) (name, version string, ok bool) {
	suffix := "." + arch + ".rpm"
	if !strings.HasSuffix(fileName, suffix) {
		return "", "", false
	}
	base := strings.TrimSuffix(fileName, suffix)
	idx := strings.LastIndex(base, "-")
	if idx < 0 {
		return "", "", false
	}
	return base[:idx], base[idx+1:], true
}

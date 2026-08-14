// Package search implements upstream search adapters for registries that
// expose a public repository search API (Docker Hub, Quay.io). Registries
// without an anonymous search API (e.g. GHCR) simply have no adapter.
package search

import "context"

// Result is a single upstream search hit, normalized across adapters.
type Result struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"starCount"`
	Official    bool   `json:"official"`
	Source      string `json:"source"`
}

// UpstreamSearcher queries an upstream registry's public search API.
type UpstreamSearcher interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

var searchers = map[string]UpstreamSearcher{
	"dockerhub": NewDockerHubSearcher(),
	"quay":      NewQuaySearcher(),
}

// Dispatch returns the UpstreamSearcher for a registry ID, if one exists.
// Registries with no public anonymous search API (e.g. "ghcr") return
// ok == false rather than an error, since this is an expected, permanent
// state rather than a transient failure.
func Dispatch(registryID string) (searcher UpstreamSearcher, ok bool) {
	s, ok := searchers[registryID]
	return s, ok
}

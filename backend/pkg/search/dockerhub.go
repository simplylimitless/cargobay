package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// dockerHubSearcher queries Docker Hub's public repository search API.
// https://hub.docker.com/v2/search/repositories/?query=<q>
type dockerHubSearcher struct {
	client *http.Client
}

func NewDockerHubSearcher() UpstreamSearcher {
	return &dockerHubSearcher{client: &http.Client{Timeout: 3 * time.Second}}
}

type dockerHubResponse struct {
	Results []struct {
		RepoName         string `json:"repo_name"`
		ShortDescription string `json:"short_description"`
		StarCount        int    `json:"star_count"`
		IsOfficial       bool   `json:"is_official"`
	} `json:"results"`
}

func (s *dockerHubSearcher) Search(ctx context.Context, query string) ([]Result, error) {
	reqURL := "https://hub.docker.com/v2/search/repositories/?query=" + url.QueryEscape(query) + "&page_size=25"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker hub search returned status %d", resp.StatusCode)
	}

	var parsed dockerHubResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		namespace, name := splitRepoName(r.RepoName)
		results = append(results, Result{
			Namespace:   namespace,
			Name:        name,
			Description: r.ShortDescription,
			StarCount:   r.StarCount,
			Official:    r.IsOfficial,
			Source:      "upstream",
		})
	}
	return results, nil
}

// splitRepoName splits a Docker Hub "repo_name" (e.g. "library/nginx" or
// "bitnami/nginx") into namespace and name. Official images have no
// namespace prefix upstream, so they're normalized to "library".
func splitRepoName(repoName string) (namespace, name string) {
	if idx := strings.Index(repoName, "/"); idx != -1 {
		return repoName[:idx], repoName[idx+1:]
	}
	return "library", repoName
}

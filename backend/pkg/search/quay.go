package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// quaySearcher queries Quay.io's public repository search API.
// https://quay.io/api/v1/find/repositories?query=<q>
type quaySearcher struct {
	client *http.Client
}

func NewQuaySearcher() UpstreamSearcher {
	return &quaySearcher{client: &http.Client{Timeout: 3 * time.Second}}
}

type quayResponse struct {
	Results []struct {
		Namespace   string `json:"namespace"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"results"`
}

func (s *quaySearcher) Search(ctx context.Context, query string) ([]Result, error) {
	reqURL := "https://quay.io/api/v1/find/repositories?query=" + url.QueryEscape(query)
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
		return nil, fmt.Errorf("quay search returned status %d", resp.StatusCode)
	}

	var parsed quayResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		results = append(results, Result{
			Namespace:   r.Namespace,
			Name:        r.Name,
			Description: r.Description,
			Source:      "upstream",
		})
	}
	return results, nil
}

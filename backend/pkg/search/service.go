package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
)

// SearchService provides artifact search capabilities
type SearchService struct {
	db          *database.Database
	esClient    *elasticsearch.Client
	indexName   string
	enabled     bool
}

// SearchResult represents a search result
type SearchResult struct {
	ID              string         `json:"id"`
	RegistryID      string         `json:"registry_id"`
	ArtifactType    string         `json:"artifact_type"`
	Namespace       string         `json:"namespace"`
	ArtifactName    string         `json:"artifact_name"`
	Version         string         `json:"version"`
	Digest          string         `json:"digest"`
	Size            int64          `json:"size"`
	Created         time.Time      `json:"created"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	Score           float64        `json:"_score"`
}

// SearchResults holds paginated search results
type SearchResults struct {
	Results  []SearchResult
	Total    int
	HitRate  float64
	Cursor   string
}

// New creates a new SearchService
func New(db *database.Database, esURL, indexName string) (*SearchService, error) {
	if esURL == "" {
		return &SearchService{
			db:        db,
			enabled:   false,
			indexName: indexName,
		}, nil
	}

	cfg := elasticsearch.Config{
		Addresses: []string{esURL},
		Username:  "", // Can be set if using basic auth
		Password:  "",
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	// Verify connection
	_, err = client.Ping()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Elasticsearch: %w", err)
	}

	return &SearchService{
		db:        db,
		esClient:  client,
		indexName: indexName,
		enabled:   true,
	}, nil
}

// IndexArtifact indexes an artifact for search
func (s *SearchService) IndexArtifact(artifact *database.ArtifactMetadata) error {
	if !s.enabled || s.esClient == nil {
		return nil
	}

	doc := map[string]interface{}{
		"id":              artifact.ID,
		"registry_id":     artifact.RegistryID,
		"artifact_type":   artifact.ArtifactType,
		"namespace":       artifact.Namespace,
		"artifact_name":   artifact.ArtifactName,
		"version":         artifact.Version,
		"digest":          artifact.Digest,
		"digest_algorithm": artifact.DigestAlgorithm,
		"size":            artifact.Size,
		"created":         artifact.Created,
		"updated":         artifact.Updated,
		"metadata":        artifact.Metadata,
		"tags":            artifact.Tags,
		"search_text":     fmt.Sprintf("%s %s %s %s", artifact.ArtifactName, artifact.Namespace, artifact.Version, strings.Join(artifact.Tags, " ")),
	}

	_, err := s.esClient.Index(
		s.indexName,
		esutil.NewJSONReader(doc),
		s.esClient.Index.WithDocumentID(artifact.ID),
		s.esClient.Index.WithRefresh("true"),
	)
	return err
}

// Search searches artifacts using Elasticsearch or PostgreSQL fallback
func (s *SearchService) Search(query string, opts database.SearchOptions) (*SearchResults, error) {
	if s.enabled && s.esClient != nil {
		return s.searchElasticsearch(query, opts)
	}
	return s.searchPostgres(query, opts)
}

// searchElasticsearch performs search using Elasticsearch
func (s *SearchService) searchElasticsearch(query string, opts database.SearchOptions) (*SearchResults, error) {
	searchBody := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":         query,
				"fields":        []string{"artifact_name^3", "namespace^2", "search_text", "version"},
				"type":          "best_fields",
				"operator":      "or",
				"boost":         1.0,
			},
		},
		"highlight": map[string]interface{}{
			"fields": map[string]interface{}{
				"artifact_name": map[string]interface{}{},
				"namespace":     map[string]interface{}{},
			},
		},
		"size": opts.Limit,
		"from": opts.Offset,
	}

	if opts.RegistryID != "" {
		searchBody["query"].(map[string]interface{})["bool"] = map[string]interface{}{
			"must": []interface{}{
				map[string]interface{}{
					"term": map[string]interface{}{
						"registry_id": opts.RegistryID,
					},
				},
			},
		}
	}

	if opts.ArtifactType != "" {
		if boolQuery, ok := searchBody["query"].(map[string]interface{})["bool"]; ok {
			boolQuery.(map[string]interface{})["must"] = append(
				boolQuery.(map[string]interface{})["must"].([]interface{}),
				map[string]interface{}{
					"term": map[string]interface{}{
						"artifact_type": opts.ArtifactType,
					},
				},
			)
		} else {
			searchBody["query"].(map[string]interface{})["bool"] = map[string]interface{}{
				"must": []interface{}{
					map[string]interface{}{
						"term": map[string]interface{}{
							"artifact_type": opts.ArtifactType,
						},
					},
				},
			}
		}
	}

	res, err := s.esClient.Search(
		s.esClient.Search.WithIndex(s.indexName),
		s.esClient.Search.WithBody(esutil.NewJSONReader(searchBody)),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Parse hits
	hits := result["hits"].(map[string]interface{})
	total := int(hits["total"].(map[string]interface{})["value"].(float64))

	rawHits, _ := hits["hits"].([]interface{})
	searchResults := make([]SearchResult, 0, len(rawHits))

	for _, hit := range rawHits {
		h := hit.(map[string]interface{})
		source := h["_source"].(map[string]interface{})

		searchResults = append(searchResults, SearchResult{
			ID:              source["id"].(string),
			RegistryID:      source["registry_id"].(string),
			ArtifactType:    source["artifact_type"].(string),
			Namespace:       source["namespace"].(string),
			ArtifactName:    source["artifact_name"].(string),
			Version:         source["version"].(string),
			Digest:          source["digest"].(string),
			Size:            int64(source["size"].(float64)),
			Created:         source["created"].(time.Time),
			Metadata:        source["metadata"].(map[string]interface{}),
			Score:           h["_score"].(float64),
		})
	}

	return &SearchResults{
		Results: searchResults,
		Total:   total,
	}, nil
}

// searchPostgres performs search using PostgreSQL full-text search
func (s *SearchService) searchPostgres(query string, opts database.SearchOptions) (*SearchResults, error) {
	artifacts, err := s.db.SearchArtifacts(query, opts)
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, len(artifacts))
	for i, a := range artifacts {
		results[i] = SearchResult{
			ID:              a.ID,
			RegistryID:      a.RegistryID,
			ArtifactType:    a.ArtifactType,
			Namespace:       a.Namespace,
			ArtifactName:    a.ArtifactName,
			Version:         a.Version,
			Digest:          a.Digest,
			Size:            a.Size,
			Created:         a.Created,
			Metadata:        a.Metadata,
		}
	}

	total, err := s.db.SearchCount(query, opts)
	if err != nil {
		total = len(results)
	}

	return &SearchResults{
		Results: results,
		Total:   total,
	}, nil
}

// Autocomplete provides autocomplete suggestions
func (s *SearchService) Autocomplete(prefix string, limit int) ([]string, error) {
	if s.enabled && s.esClient != nil {
		return s.autocompleteElasticsearch(prefix, limit)
	}
	return s.autocompletePostgres(prefix, limit)
}

// autocompleteElasticsearch provides autocomplete via Elasticsearch
func (s *SearchService) autocompleteElasticsearch(prefix string, limit int) ([]string, error) {
	searchBody := map[string]interface{}{
		"size": 0,
		"aggs": map[string]interface{}{
			"autocomplete": map[string]interface{}{
				"term": map[string]interface{}{
					"field":         "artifact_name.keyword",
					"size":          limit,
					"include":       fmt.Sprintf(".*%s.*", prefix),
					"order":         map[string]interface{}{"_count": "desc"},
				},
			},
		},
	}

	res, err := s.esClient.Search(
		s.esClient.Search.WithIndex(s.indexName),
		s.esClient.Search.WithBody(esutil.NewJSONReader(searchBody)),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Parse aggregations
	buckets := result["aggregations"].(map[string]interface{})["autocomplete"].(map[string]interface{})["buckets"].([]interface{})
	suggestions := make([]string, len(buckets))

	for i, bucket := range buckets {
		b := bucket.(map[string]interface{})
		suggestions[i] = b["key"].(string)
	}

	return suggestions, nil
}

// autocompletePostgres provides autocomplete via PostgreSQL
func (s *SearchService) autocompletePostgres(prefix string, limit int) ([]string, error) {
	query := `
		SELECT DISTINCT artifact_name
		FROM artifacts
		WHERE artifact_name ILIKE $1
		ORDER BY artifact_name
		LIMIT $2
	`

	rows, err := s.db.Query(context.Background(), query, fmt.Sprintf("%s%%", prefix), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var suggestions []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, name)
	}

	return suggestions, rows.Err()
}

// DeleteArtifact removes an artifact from search index
func (s *SearchService) DeleteArtifact(id string) error {
	if !s.enabled || s.esClient == nil {
		return nil
	}

	_, err := s.esClient.Delete(
		s.indexName,
		id,
		s.esClient.Delete.WithRefresh("true"),
	)
	return err
}

// CreateIndex creates the search index
func (s *SearchService) CreateIndex() error {
	if !s.enabled || s.esClient == nil {
		return nil
	}

	// Check if index exists
	existsRes, err := s.esClient.Indices.Exists([]string{s.indexName})
	if err != nil {
		return err
	}
	defer existsRes.Body.Close()
	if existsRes.StatusCode == 200 {
		return nil
	}

	// Create index with mapping
	mapping := map[string]interface{}{
		"settings": map[string]interface{}{
			"number_of_shards":   1,
			"number_of_replicas": 0,
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"id":              map[string]interface{}{"type": "keyword"},
				"registry_id":     map[string]interface{}{"type": "keyword"},
				"artifact_type":   map[string]interface{}{"type": "keyword"},
				"namespace":       map[string]interface{}{"type": "keyword"},
				"artifact_name": map[string]interface{}{
					"type":        "text",
					"fields": map[string]interface{}{
						"keyword": map[string]interface{}{"type": "keyword"},
					},
				},
				"version":         map[string]interface{}{"type": "keyword"},
				"digest":          map[string]interface{}{"type": "keyword"},
				"size":            map[string]interface{}{"type": "long"},
				"created":         map[string]interface{}{"type": "date"},
				"updated":         map[string]interface{}{"type": "date"},
				"tags":            map[string]interface{}{"type": "keyword"},
				"search_text":     map[string]interface{}{"type": "text", "analyzer": "standard"},
			},
		},
	}

	_, err = s.esClient.Indices.Create(
		s.indexName,
		s.esClient.Indices.Create.WithBody(esutil.NewJSONReader(mapping)),
	)
	return err
}

// SyncFromDB syncs all artifacts from the database to Elasticsearch
func (s *SearchService) SyncFromDB() error {
	if !s.enabled || s.esClient == nil {
		return nil
	}

	opts := database.SearchOptions{Limit: 1000}
	bulk, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:        s.esClient,
		Index:         s.indexName,
		FlushInterval: 1 * time.Second,
		NumWorkers:    4,
		FlushBytes:    1000000,
	})
	if err != nil {
		return err
	}

	for {
		artifacts, err := s.db.SearchArtifacts("", opts)
		if err != nil {
			return err
		}

		if len(artifacts) == 0 {
			break
		}

		for _, artifact := range artifacts {
			doc := map[string]interface{}{
				"id":              artifact.ID,
				"registry_id":     artifact.RegistryID,
				"artifact_type":   artifact.ArtifactType,
				"namespace":       artifact.Namespace,
				"artifact_name":   artifact.ArtifactName,
				"version":         artifact.Version,
				"digest":          artifact.Digest,
				"size":            artifact.Size,
				"created":         artifact.Created,
				"updated":         artifact.Updated,
				"metadata":        artifact.Metadata,
				"tags":            artifact.Tags,
				"search_text":     fmt.Sprintf("%s %s %s %s", artifact.ArtifactName, artifact.Namespace, artifact.Version, strings.Join(artifact.Tags, " ")),
			}

			docBytes, err := json.Marshal(doc)
			if err != nil {
				return err
			}

			err = bulk.Add(context.Background(), esutil.BulkIndexerItem{
				Action:     "index",
				DocumentID: artifact.ID,
				Body:       bytes.NewReader(docBytes),
				OnSuccess: func(ctx context.Context, item esutil.BulkIndexerItem, response esutil.BulkIndexerResponseItem) {
					// Item indexed successfully
				},
				OnFailure: func(ctx context.Context, item esutil.BulkIndexerItem, response esutil.BulkIndexerResponseItem, err error) {
					// Handle failure
				},
			})

			if err != nil {
				return err
			}
		}

		opts.Offset += len(artifacts)
	}

	return bulk.Close(context.Background())
}

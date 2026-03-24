package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	es7 "github.com/elastic/go-elasticsearch/v7"
	es8 "github.com/elastic/go-elasticsearch/v8"
	"opsplatform-alert-backend/models"
)

// Client wraps both ES 7.x and 8.x clients
type Client struct {
	v7     *es7.Client
	v8     *es8.Client
	config models.ESConnection
}

// NewClient creates a new ES client based on version
func NewClient(conn models.ESConnection) (*Client, error) {
	urls := strings.Split(conn.URL, ",")
	for i := range urls {
		urls[i] = strings.TrimSpace(urls[i])
	}

	c := &Client{config: conn}

	switch conn.Version {
	case "7":
		cfg := es7.Config{
			Addresses: urls,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 10,
			},
		}
		if conn.Username != "" {
			cfg.Username = conn.Username
			cfg.Password = conn.Password
		}
		client, err := es7.NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create ES7 client: %w", err)
		}
		c.v7 = client

	case "8":
		cfg := es8.Config{
			Addresses: urls,
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 10,
			},
		}
		if conn.APIKey != "" {
			cfg.APIKey = conn.APIKey
		} else if conn.Username != "" {
			cfg.Username = conn.Username
			cfg.Password = conn.Password
		}
		client, err := es8.NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create ES8 client: %w", err)
		}
		c.v8 = client

	default:
		return nil, fmt.Errorf("unsupported ES version: %s", conn.Version)
	}

	return c, nil
}

// Ping tests the connection
func (c *Client) Ping(ctx context.Context) error {
	if c.v7 != nil {
		res, err := c.v7.Ping(c.v7.Ping.WithContext(ctx))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.IsError() {
			return fmt.Errorf("ES7 ping error: %s", res.Status())
		}
		return nil
	}
	if c.v8 != nil {
		res, err := c.v8.Ping(c.v8.Ping.WithContext(ctx))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.IsError() {
			return fmt.Errorf("ES8 ping error: %s", res.Status())
		}
		return nil
	}
	return fmt.Errorf("no ES client initialized")
}

// SearchResult represents parsed ES search result
type SearchResult struct {
	Hits  []map[string]interface{} `json:"hits"`
	Total int64                    `json:"total"`
}

// Search executes a search query against ES
func (c *Client) Search(ctx context.Context, index string, query map[string]interface{}) (*SearchResult, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	log.Printf("[ES] Searching index=%s query=%s", index, string(body))

	var respBody []byte

	if c.v7 != nil {
		res, err := c.v7.Search(
			c.v7.Search.WithContext(ctx),
			c.v7.Search.WithIndex(index),
			c.v7.Search.WithBody(bytes.NewReader(body)),
			c.v7.Search.WithTrackTotalHits(true),
		)
		if err != nil {
			return nil, fmt.Errorf("ES7 search error: %w", err)
		}
		defer res.Body.Close()
		if res.IsError() {
			b, _ := io.ReadAll(res.Body)
			return nil, fmt.Errorf("ES7 search error: %s body=%s", res.Status(), string(b))
		}
		respBody, err = io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
	} else if c.v8 != nil {
		res, err := c.v8.Search(
			c.v8.Search.WithContext(ctx),
			c.v8.Search.WithIndex(index),
			c.v8.Search.WithBody(bytes.NewReader(body)),
			c.v8.Search.WithTrackTotalHits(true),
		)
		if err != nil {
			return nil, fmt.Errorf("ES8 search error: %w", err)
		}
		defer res.Body.Close()
		if res.IsError() {
			b, _ := io.ReadAll(res.Body)
			return nil, fmt.Errorf("ES8 search error: %s body=%s", res.Status(), string(b))
		}
		respBody, err = io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
	}

	return parseSearchResponse(respBody)
}

func parseSearchResponse(body []byte) (*SearchResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse ES response: %w", err)
	}

	result := &SearchResult{}

	hits, ok := raw["hits"].(map[string]interface{})
	if !ok {
		return result, nil
	}

	// Parse total
	switch total := hits["total"].(type) {
	case map[string]interface{}:
		if v, ok := total["value"].(float64); ok {
			result.Total = int64(v)
		}
	case float64:
		result.Total = int64(total)
	}

	// Parse hits array
	hitList, ok := hits["hits"].([]interface{})
	if !ok {
		return result, nil
	}

	for _, h := range hitList {
		hit, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		doc := make(map[string]interface{})
		// Add _id
		if id, ok := hit["_id"].(string); ok {
			doc["_id"] = id
		}
		// Add _index
		if idx, ok := hit["_index"].(string); ok {
			doc["_index"] = idx
		}
		// Merge _source
		if source, ok := hit["_source"].(map[string]interface{}); ok {
			for k, v := range source {
				doc[k] = v
			}
		}
		result.Hits = append(result.Hits, doc)
	}

	return result, nil
}

// BuildQuery builds an ES query from alert rule config
func BuildQuery(keyword string, filterFieldsJSON string, timeRange string, queryDSL string, maxAlerts int) (map[string]interface{}, error) {
	// If custom DSL is provided, use it directly
	if queryDSL != "" {
		var query map[string]interface{}
		if err := json.Unmarshal([]byte(queryDSL), &query); err != nil {
			return nil, fmt.Errorf("invalid query DSL: %w", err)
		}
		// Ensure size is set
		if _, ok := query["size"]; !ok {
			query["size"] = maxAlerts
		}
		return query, nil
	}

	// Build query from keyword + filters
	must := []map[string]interface{}{}

	// Time range filter
	if timeRange != "" {
		must = append(must, map[string]interface{}{
			"range": map[string]interface{}{
				"@timestamp": map[string]interface{}{
					"gte": fmt.Sprintf("now-%s", timeRange),
					"lte": "now",
				},
			},
		})
	}

	// Keyword search
	if keyword != "" {
		must = append(must, map[string]interface{}{
			"query_string": map[string]interface{}{
				"query":            keyword,
				"default_operator": "AND",
			},
		})
	}

	// Additional filter fields
	if filterFieldsJSON != "" {
		var filters []models.FilterField
		if err := json.Unmarshal([]byte(filterFieldsJSON), &filters); err != nil {
			log.Printf("[ES] Warning: failed to parse filter_fields: %v", err)
		} else {
			for _, f := range filters {
				op := f.Op
				if op == "" {
					op = "match"
				}
				switch op {
				case "term":
					must = append(must, map[string]interface{}{
						"term": map[string]interface{}{f.Field: f.Value},
					})
				case "wildcard":
					must = append(must, map[string]interface{}{
						"wildcard": map[string]interface{}{f.Field: f.Value},
					})
				case "exists":
					must = append(must, map[string]interface{}{
						"exists": map[string]interface{}{"field": f.Field},
					})
				default: // match
					must = append(must, map[string]interface{}{
						"match": map[string]interface{}{f.Field: f.Value},
					})
				}
			}
		}
	}

	query := map[string]interface{}{
		"size": maxAlerts,
		"sort": []map[string]interface{}{
			{"@timestamp": map[string]interface{}{"order": "desc"}},
		},
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": must,
			},
		},
	}

	return query, nil
}

// GetNestedField extracts a nested field value from a map using dot notation
func GetNestedField(doc map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = doc

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		default:
			return nil
		}
	}
	return current
}

// ParseTimeRange parses time range string like "5m", "1h", "30s" to duration
func ParseTimeRange(s string) time.Duration {
	if s == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}
